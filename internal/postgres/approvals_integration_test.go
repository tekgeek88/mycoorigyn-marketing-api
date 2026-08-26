package postgres

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/approvals"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/securetokens"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/submissions"
)

func TestSignupGrantConcurrentClaimAndConsume(t *testing.T) {
	store, pool := integrationStore(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	reviewToken, submissionID := insertPendingReview(t, pool, now, "concurrent@example.com")
	defer cleanupApprovalFixture(t, pool, submissionID)

	grantToken, grantDigest, err := securetokens.Generate()
	if err != nil {
		t.Fatal(err)
	}
	approval, used, err := store.Approve(context.Background(), securetokens.Digest(reviewToken), approvals.GrantCandidate{
		TokenDigest: grantDigest, SecretReference: "signup/integration", ExpiresAt: now.Add(time.Hour),
	}, now)
	if err != nil || !used {
		t.Fatalf("approve used=%v err=%v", used, err)
	}
	if approval.ApprovedEmail != "concurrent@example.com" {
		t.Fatalf("approved email = %q", approval.ApprovedEmail)
	}

	var digestBytes int
	var storedReference string
	if err := pool.QueryRow(context.Background(), `
		SELECT octet_length(token_digest), secret_reference
		FROM signup_grants WHERE id = $1::uuid
	`, approval.GrantID).Scan(&digestBytes, &storedReference); err != nil {
		t.Fatal(err)
	}
	if digestBytes != 32 || storedReference == grantToken {
		t.Fatalf("plaintext grant was persisted")
	}

	if grant, err := store.ValidateGrant(context.Background(), securetokens.Digest(grantToken), "concurrent@example.com", now); err != nil || grant.Status != "active" {
		t.Fatalf("validate = %#v err=%v", grant, err)
	}
	metadata, err := store.ResolveGrant(context.Background(), securetokens.Digest(grantToken), now)
	if err != nil {
		t.Fatal(err)
	}
	if metadata.ApprovedEmail != "concurrent@example.com" || metadata.OwnerName != "Integration Owner" || metadata.FarmName != "Integration Farm" || metadata.Status != "active" {
		t.Fatalf("resolved metadata = %#v", metadata)
	}
	var statusAfterResolve string
	var claimAfterResolve []byte
	if err := pool.QueryRow(context.Background(), `
		SELECT status, COALESCE(claim_reference_digest, ''::bytea)
		FROM signup_grants WHERE id = $1::uuid
	`, approval.GrantID).Scan(&statusAfterResolve, &claimAfterResolve); err != nil {
		t.Fatal(err)
	}
	if statusAfterResolve != "active" || len(claimAfterResolve) != 0 {
		t.Fatalf("metadata resolution mutated grant: status=%q claim_set=%t", statusAfterResolve, len(claimAfterResolve) != 0)
	}

	type result struct {
		claim string
		err   error
	}
	start := make(chan struct{})
	results := make(chan result, 2)
	var wg sync.WaitGroup
	for _, claim := range []string{"provision-operation-a", "provision-operation-b"} {
		claim := claim
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := store.ClaimGrant(context.Background(), securetokens.Digest(grantToken), "concurrent@example.com", securetokens.Digest(claim), now, now.Add(time.Minute))
			results <- result{claim: claim, err: err}
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	winner := ""
	claimSuccesses := 0
	for result := range results {
		if result.err == nil {
			winner = result.claim
			claimSuccesses++
		} else if !errors.Is(result.err, approvals.ErrGrantClaimed) {
			t.Fatalf("unexpected competing claim error: %v", result.err)
		}
	}
	if claimSuccesses != 1 {
		t.Fatalf("claim successes = %d, want 1", claimSuccesses)
	}

	consumeResults := make(chan error, 2)
	start = make(chan struct{})
	for range 2 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			<-start
			_, err := store.ConsumeGrant(context.Background(), securetokens.Digest(grantToken), "concurrent@example.com", securetokens.Digest(winner), now.Add(time.Second))
			consumeResults <- err
		}()
	}
	close(start)
	wg.Wait()
	close(consumeResults)
	consumeSuccesses := 0
	for err := range consumeResults {
		if err == nil {
			consumeSuccesses++
		} else {
			t.Fatalf("unexpected concurrent consume error: %v", err)
		}
	}
	if consumeSuccesses != 2 {
		t.Fatalf("same-claim consume successes = %d, want 2", consumeSuccesses)
	}
	if _, err := store.ValidateGrant(context.Background(), securetokens.Digest(grantToken), "concurrent@example.com", now.Add(2*time.Second)); !errors.Is(err, approvals.ErrInvalidGrant) {
		t.Fatalf("consumed grant validated: %v", err)
	}
}

func TestConsumeGrantReplayRetainsConsumingClaimAndOriginalTimestamps(t *testing.T) {
	store, pool := integrationStore(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	reviewToken, submissionID := insertPendingReview(t, pool, now, "replay@example.com")
	defer cleanupApprovalFixture(t, pool, submissionID)

	_, grantDigest, err := securetokens.Generate()
	if err != nil {
		t.Fatal(err)
	}
	approval, _, err := store.Approve(context.Background(), securetokens.Digest(reviewToken), approvals.GrantCandidate{
		TokenDigest: grantDigest, SecretReference: "signup/replay", ExpiresAt: now.Add(24 * time.Hour),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	claimA := securetokens.Digest("stable-provision-operation-a")
	claimB := securetokens.Digest("stable-provision-operation-b")
	if _, err := store.ClaimGrant(context.Background(), grantDigest, "replay@example.com", claimA, now, now.Add(time.Minute)); err != nil {
		t.Fatal(err)
	}

	first, err := store.ConsumeGrant(context.Background(), grantDigest, "replay@example.com", claimA, now.Add(10*time.Second))
	if err != nil || first.Status != "consumed" || first.ID != approval.GrantID {
		t.Fatalf("first consume = %#v, err=%v", first, err)
	}
	var firstConsumedAt, firstUpdatedAt time.Time
	var retainedDigest []byte
	var claimedAtCleared, claimExpiryCleared bool
	if err := pool.QueryRow(context.Background(), `
		SELECT consumed_at, updated_at, claim_reference_digest,
			claimed_at IS NULL, claim_expires_at IS NULL
		FROM signup_grants WHERE id = $1::uuid
	`, approval.GrantID).Scan(&firstConsumedAt, &firstUpdatedAt, &retainedDigest, &claimedAtCleared, &claimExpiryCleared); err != nil {
		t.Fatal(err)
	}
	if subtle.ConstantTimeCompare(retainedDigest, claimA) != 1 || !claimedAtCleared || !claimExpiryCleared {
		t.Fatalf("consumed claim proof or cleared lease fields are invalid")
	}

	// Simulate commit success followed by response loss, then retry after the former lease expired.
	second, err := store.ConsumeGrant(context.Background(), grantDigest, "replay@example.com", claimA, now.Add(2*time.Hour))
	if err != nil || second.Status != "consumed" || second.ID != first.ID {
		t.Fatalf("replayed consume = %#v, err=%v", second, err)
	}
	var secondConsumedAt, secondUpdatedAt time.Time
	if err := pool.QueryRow(context.Background(), `
		SELECT consumed_at, updated_at FROM signup_grants WHERE id = $1::uuid
	`, approval.GrantID).Scan(&secondConsumedAt, &secondUpdatedAt); err != nil {
		t.Fatal(err)
	}
	if !secondConsumedAt.Equal(firstConsumedAt) || !secondUpdatedAt.Equal(firstUpdatedAt) {
		t.Fatalf("idempotent replay changed timestamps: consumed %s -> %s, updated %s -> %s",
			firstConsumedAt, secondConsumedAt, firstUpdatedAt, secondUpdatedAt)
	}
	if _, err := store.ConsumeGrant(context.Background(), grantDigest, "replay@example.com", claimB, now.Add(3*time.Hour)); !errors.Is(err, approvals.ErrInvalidClaim) {
		t.Fatalf("different claim replay error = %v", err)
	}
	if _, err := store.ValidateGrant(context.Background(), grantDigest, "replay@example.com", now.Add(2*time.Hour)); !errors.Is(err, approvals.ErrInvalidGrant) {
		t.Fatalf("consumed grant validated: %v", err)
	}
	if _, err := store.ClaimGrant(context.Background(), grantDigest, "replay@example.com", claimA, now.Add(2*time.Hour), now.Add(3*time.Hour)); !errors.Is(err, approvals.ErrInvalidGrant) {
		t.Fatalf("consumed grant reclaimed: %v", err)
	}
	if _, err := store.ReleaseGrant(context.Background(), grantDigest, "replay@example.com", claimA, now.Add(2*time.Hour)); !errors.Is(err, approvals.ErrInvalidClaim) {
		t.Fatalf("consumed grant released: %v", err)
	}
	var grantCount int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM signup_grants WHERE early_access_submission_id = $1::uuid`, submissionID).Scan(&grantCount); err != nil {
		t.Fatal(err)
	}
	if grantCount != 1 {
		t.Fatalf("grant count = %d, want 1", grantCount)
	}
}

func TestDeclineIsIdempotentAndCreatesNoGrant(t *testing.T) {
	store, pool := integrationStore(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	reviewToken, submissionID := insertPendingReview(t, pool, now, "decline@example.com")
	defer cleanupApprovalFixture(t, pool, submissionID)
	if err := store.Decline(context.Background(), securetokens.Digest(reviewToken), now); err != nil {
		t.Fatal(err)
	}
	if err := store.Decline(context.Background(), securetokens.Digest(reviewToken), now); err != nil {
		t.Fatal(err)
	}
	var grants int
	if err := pool.QueryRow(context.Background(), `SELECT count(*) FROM signup_grants WHERE early_access_submission_id = $1::uuid`, submissionID).Scan(&grants); err != nil {
		t.Fatal(err)
	}
	if grants != 0 {
		t.Fatalf("decline created %d grants", grants)
	}
}

func TestGrantRejectsWrongEmailExpiredAndRevoked(t *testing.T) {
	store, pool := integrationStore(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	reviewToken, submissionID := insertPendingReview(t, pool, now, "lifecycle@example.com")
	defer cleanupApprovalFixture(t, pool, submissionID)
	grantToken, grantDigest, err := securetokens.Generate()
	if err != nil {
		t.Fatal(err)
	}
	approval, _, err := store.Approve(context.Background(), securetokens.Digest(reviewToken), approvals.GrantCandidate{
		TokenDigest: grantDigest, SecretReference: "signup/lifecycle", ExpiresAt: now.Add(time.Hour),
	}, now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.ValidateGrant(context.Background(), securetokens.Digest(grantToken), "wrong@example.com", now); !errors.Is(err, approvals.ErrInvalidGrant) {
		t.Fatalf("wrong email error = %v", err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE signup_grants SET status = 'expired' WHERE id = $1::uuid`, approval.GrantID); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ValidateGrant(context.Background(), securetokens.Digest(grantToken), "lifecycle@example.com", now); !errors.Is(err, approvals.ErrGrantExpired) {
		t.Fatalf("expired error = %v", err)
	}
	if _, err := pool.Exec(context.Background(), `UPDATE signup_grants SET status = 'revoked', revoked_at = $2 WHERE id = $1::uuid`, approval.GrantID, now); err != nil {
		t.Fatal(err)
	}
	if _, err := store.ValidateGrant(context.Background(), securetokens.Digest(grantToken), "lifecycle@example.com", now); !errors.Is(err, approvals.ErrInvalidGrant) {
		t.Fatalf("revoked error = %v", err)
	}
}

func TestConcurrentDuplicateSubmissionHasOneWinner(t *testing.T) {
	store, pool := integrationStore(t)
	now := time.Now().UTC().Truncate(time.Microsecond)
	fingerprint := fmt.Sprintf("duplicate-%d", now.UnixNano())
	submission := submissions.Submission{
		SubmissionType: submissions.TypeEarlyAccess, Email: "duplicate@example.com",
		Source: submissions.DefaultSource, Status: submissions.DefaultStatus, PayloadFingerprint: fingerprint,
	}
	start := make(chan struct{})
	results := make(chan submissions.CreateResult, 2)
	errs := make(chan error, 2)
	var wg sync.WaitGroup
	for i := range 2 {
		i := i
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, digest, err := securetokens.Generate()
			if err != nil {
				errs <- err
				return
			}
			<-start
			result, err := store.CreateSubmission(context.Background(), submission, now.Add(-time.Hour), &submissions.ReviewCapability{
				TokenDigest: digest, SecretReference: fmt.Sprintf("review/duplicate-%d", i), ExpiresAt: now.Add(time.Hour),
			})
			if err != nil {
				errs <- err
				return
			}
			results <- result
		}()
	}
	close(start)
	wg.Wait()
	close(results)
	close(errs)
	for err := range errs {
		t.Fatal(err)
	}
	created := 0
	var submissionID string
	for result := range results {
		if result.Created {
			created++
		}
		submissionID = result.SubmissionID
	}
	if created != 1 {
		t.Fatalf("created results = %d, want 1", created)
	}
	cleanupApprovalFixture(t, pool, submissionID)
}

func integrationStore(t *testing.T) (Store, *pgxpool.Pool) {
	t.Helper()
	if os.Getenv("MARKETING_OPERATIONAL_TEST") != "1" {
		t.Skip("set MARKETING_OPERATIONAL_TEST=1 to run PostgreSQL integration tests")
	}
	dsn := os.Getenv("TEST_DATABASE_URL")
	if dsn == "" {
		t.Fatal("TEST_DATABASE_URL is required")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	if err := pool.Ping(context.Background()); err != nil {
		t.Fatal(err)
	}
	return NewStore(pool), pool
}

func insertPendingReview(t *testing.T, pool *pgxpool.Pool, now time.Time, email string) (string, string) {
	t.Helper()
	reviewToken, reviewDigest, err := securetokens.Generate()
	if err != nil {
		t.Fatal(err)
	}
	fingerprint := fmt.Sprintf("integration-%d", now.UnixNano())
	var submissionID string
	err = pool.QueryRow(context.Background(), `
		INSERT INTO early_access_submissions (
			submission_type, email, name, farm_name, source, status, payload_fingerprint, approval_status
		) VALUES ('early_access', $1, 'Integration Owner', 'Integration Farm', 'integration_test', 'new', $2, 'pending')
		RETURNING id::text
	`, email, fingerprint).Scan(&submissionID)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(context.Background(), `
		INSERT INTO early_access_review_capabilities (
			early_access_submission_id, token_digest, secret_reference, expires_at
		) VALUES ($1::uuid, $2, $3, $4)
	`, submissionID, reviewDigest, "review/"+fingerprint, now.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	return reviewToken, submissionID
}

func cleanupApprovalFixture(t *testing.T, pool *pgxpool.Pool, submissionID string) {
	t.Helper()
	ctx := context.Background()
	if _, err := pool.Exec(ctx, `DELETE FROM signup_grants WHERE early_access_submission_id = $1::uuid`, submissionID); err != nil {
		t.Logf("cleanup signup grant: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM early_access_review_capabilities WHERE early_access_submission_id = $1::uuid`, submissionID); err != nil {
		t.Logf("cleanup review capability: %v", err)
	}
	if _, err := pool.Exec(ctx, `DELETE FROM early_access_submissions WHERE id = $1::uuid`, submissionID); err != nil {
		t.Logf("cleanup submission: %v", err)
	}
}

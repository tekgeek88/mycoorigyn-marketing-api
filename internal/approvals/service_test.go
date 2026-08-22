package approvals

import (
	"context"
	"crypto/subtle"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/securetokens"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/transactionalemail"
)

type fakeRepository struct {
	mu           sync.Mutex
	application  Application
	reviewDigest []byte
	reviewStatus string
	approval     *Approval
	grant        Grant
	claimDigest  []byte
}

func newApprovalFixture(t *testing.T) (*fakeRepository, string, time.Time) {
	t.Helper()
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	reviewToken, reviewDigest, err := securetokens.Generate()
	if err != nil {
		t.Fatal(err)
	}
	return &fakeRepository{
		application: Application{
			ID: "submission-1", Name: "<Owner>", Email: "owner@example.com",
			FarmName: "Farm & Fungi", ApprovalStatus: "pending", ReviewExpiresAt: now.Add(time.Hour),
		},
		reviewDigest: reviewDigest,
		reviewStatus: "active",
	}, reviewToken, now
}

func (f *fakeRepository) ResolveReview(_ context.Context, digest []byte, now time.Time) (Application, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if subtle.ConstantTimeCompare(digest, f.reviewDigest) != 1 {
		return Application{}, ErrInvalidReview
	}
	if !now.Before(f.application.ReviewExpiresAt) {
		return Application{}, ErrReviewExpired
	}
	if f.reviewStatus != "active" || f.application.ApprovalStatus != "pending" {
		return Application{}, ErrInvalidReview
	}
	return f.application, nil
}

func (f *fakeRepository) Approve(_ context.Context, digest []byte, candidate GrantCandidate, now time.Time) (Approval, bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if subtle.ConstantTimeCompare(digest, f.reviewDigest) != 1 {
		return Approval{}, false, ErrInvalidReview
	}
	if !now.Before(f.application.ReviewExpiresAt) {
		return Approval{}, false, ErrReviewExpired
	}
	if f.application.ApprovalStatus == "declined" {
		return Approval{}, false, ErrDecisionConflict
	}
	if f.approval != nil {
		return *f.approval, false, nil
	}
	f.application.ApprovalStatus = "approved"
	f.reviewStatus = "decided"
	f.approval = &Approval{
		ApplicationID: f.application.ID, GrantID: "grant-1", ApprovedEmail: f.application.Email,
		FarmName: f.application.FarmName, TokenDigest: append([]byte(nil), candidate.TokenDigest...),
		SecretReference: candidate.SecretReference, ExpiresAt: candidate.ExpiresAt,
	}
	f.grant = Grant{ID: "grant-1", ApprovedEmail: f.application.Email, Source: GrantSourceEarlyAccess, Status: "active", ExpiresAt: candidate.ExpiresAt}
	return *f.approval, true, nil
}

func (f *fakeRepository) Decline(_ context.Context, digest []byte, now time.Time) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if subtle.ConstantTimeCompare(digest, f.reviewDigest) != 1 {
		return ErrInvalidReview
	}
	if !now.Before(f.application.ReviewExpiresAt) {
		return ErrReviewExpired
	}
	if f.application.ApprovalStatus == "approved" {
		return ErrDecisionConflict
	}
	f.application.ApprovalStatus = "declined"
	f.reviewStatus = "decided"
	return nil
}

func (f *fakeRepository) ValidateGrant(_ context.Context, digest []byte, email string, now time.Time) (Grant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.approval == nil || subtle.ConstantTimeCompare(digest, f.approval.TokenDigest) != 1 || email != f.grant.ApprovedEmail {
		return Grant{}, ErrInvalidGrant
	}
	if !now.Before(f.grant.ExpiresAt) {
		return Grant{}, ErrGrantExpired
	}
	if f.grant.Status != "active" && f.grant.Status != "claimed" {
		return Grant{}, ErrInvalidGrant
	}
	return f.grant, nil
}

func (f *fakeRepository) ClaimGrant(ctx context.Context, digest []byte, email string, claimDigest []byte, now, claimExpiresAt time.Time) (Grant, error) {
	if _, err := f.ValidateGrant(ctx, digest, email, now); err != nil {
		return Grant{}, err
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.grant.Status == "claimed" && subtle.ConstantTimeCompare(f.claimDigest, claimDigest) != 1 && now.Before(f.grant.ClaimExpiresAt) {
		return Grant{}, ErrGrantClaimed
	}
	f.grant.Status = "claimed"
	f.grant.ClaimExpiresAt = claimExpiresAt
	f.claimDigest = append([]byte(nil), claimDigest...)
	return f.grant, nil
}

func (f *fakeRepository) ConsumeGrant(_ context.Context, digest []byte, email string, claimDigest []byte, now time.Time) (Grant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.approval == nil || subtle.ConstantTimeCompare(digest, f.approval.TokenDigest) != 1 || email != f.grant.ApprovedEmail ||
		f.grant.Status != "claimed" || subtle.ConstantTimeCompare(f.claimDigest, claimDigest) != 1 || !now.Before(f.grant.ClaimExpiresAt) {
		return Grant{}, ErrInvalidClaim
	}
	f.grant.Status = "consumed"
	return f.grant, nil
}

func (f *fakeRepository) ReleaseGrant(_ context.Context, digest []byte, email string, claimDigest []byte, _ time.Time) (Grant, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.approval == nil || subtle.ConstantTimeCompare(digest, f.approval.TokenDigest) != 1 || email != f.grant.ApprovedEmail || f.grant.Status != "claimed" || subtle.ConstantTimeCompare(f.claimDigest, claimDigest) != 1 {
		return Grant{}, ErrInvalidClaim
	}
	f.grant.Status = "active"
	f.grant.ClaimExpiresAt = time.Time{}
	f.claimDigest = nil
	return f.grant, nil
}

func TestApprovalCreatesOneGrantAndResendReusesToken(t *testing.T) {
	repo, reviewToken, now := newApprovalFixture(t)
	tokens := securetokens.NewMemoryStore()
	sender := &transactionalemail.MemorySender{}
	service := NewService(repo, ServiceOptions{
		Tokens: tokens, Email: sender, From: "MycoOrigyn <notify@example.com>",
		SignupBaseURL: "https://app.example.com/signup", Now: func() time.Time { return now },
	})

	first, err := service.Approve(context.Background(), reviewToken)
	if err != nil || first.Status != "approved" || first.DeliveryStatus != "delivered" {
		t.Fatalf("first approval = %#v, err=%v", first, err)
	}
	second, err := service.Approve(context.Background(), reviewToken)
	if err != nil || second.Status != "approved" {
		t.Fatalf("second approval = %#v, err=%v", second, err)
	}
	messages := sender.Snapshot()
	if len(messages) != 2 || messages[0].Text != messages[1].Text || messages[0].HTML != messages[1].HTML {
		t.Fatalf("approval retry did not reuse the exact grant message")
	}
	if !strings.Contains(messages[0].Text, "/signup#access=") || strings.Contains(messages[0].Text, "?access=") {
		t.Fatalf("signup URL is not fragment-based")
	}
	if strings.Contains(messages[0].HTML, "<Owner>") || strings.Contains(messages[0].HTML, "Farm & Fungi") {
		t.Fatalf("approval HTML did not escape dynamic content")
	}
	if repo.approval == nil || len(repo.approval.TokenDigest) != 32 || strings.Contains(repo.approval.SecretReference, "#access=") {
		t.Fatalf("unsafe durable grant model: %#v", repo.approval)
	}
}

func TestApprovalDeliveryFailurePreservesGrantForRetry(t *testing.T) {
	repo, reviewToken, now := newApprovalFixture(t)
	sender := &transactionalemail.MemorySender{Err: errors.New("provider unavailable")}
	service := NewService(repo, ServiceOptions{
		Tokens: securetokens.NewMemoryStore(), Email: sender, From: "notify@example.com",
		SignupBaseURL: "https://app.example.com/signup", Now: func() time.Time { return now },
	})
	result, err := service.Approve(context.Background(), reviewToken)
	if !errors.Is(err, ErrDeliveryFailed) || result.Status != "approved" || repo.approval == nil {
		t.Fatalf("durable failure result = %#v, err=%v", result, err)
	}
	sender.Err = nil
	if _, err := service.Approve(context.Background(), reviewToken); err != nil {
		t.Fatalf("retry failed: %v", err)
	}
	if repo.approval.GrantID != "grant-1" || len(sender.Snapshot()) != 1 {
		t.Fatalf("retry created a different grant or did not deliver")
	}
}

func TestDeclineIsIdempotentAndCreatesNoGrant(t *testing.T) {
	repo, reviewToken, now := newApprovalFixture(t)
	service := NewService(repo, ServiceOptions{Now: func() time.Time { return now }})
	if err := service.Decline(context.Background(), reviewToken); err != nil {
		t.Fatal(err)
	}
	if err := service.Decline(context.Background(), reviewToken); err != nil {
		t.Fatal(err)
	}
	if repo.approval != nil || repo.application.ApprovalStatus != "declined" {
		t.Fatalf("decline state is unsafe")
	}
}

func TestGrantClaimReleaseConsumeContract(t *testing.T) {
	repo, reviewToken, now := newApprovalFixture(t)
	service := NewService(repo, ServiceOptions{
		Tokens: securetokens.NewMemoryStore(), Email: &transactionalemail.MemorySender{}, From: "notify@example.com",
		SignupBaseURL: "https://app.example.com/signup", Now: func() time.Time { return now }, ClaimLifetime: time.Minute,
	})
	if _, err := service.Approve(context.Background(), reviewToken); err != nil {
		t.Fatal(err)
	}
	token, err := service.tokens.Read(context.Background(), repo.approval.SecretReference)
	if err != nil {
		t.Fatal(err)
	}
	if grant, err := service.ValidateGrant(context.Background(), token, " OWNER@EXAMPLE.COM "); err != nil || grant.Status != "active" {
		t.Fatalf("validate = %#v, err=%v", grant, err)
	}
	if _, err := service.ClaimGrant(context.Background(), token, "owner@example.com", "provision-operation-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ClaimGrant(context.Background(), token, "owner@example.com", "provision-operation-2"); !errors.Is(err, ErrGrantClaimed) {
		t.Fatalf("competing claim error = %v", err)
	}
	if _, err := service.ReleaseGrant(context.Background(), token, "owner@example.com", "provision-operation-1"); err != nil {
		t.Fatal(err)
	}
	if _, err := service.ClaimGrant(context.Background(), token, "owner@example.com", "provision-operation-2"); err != nil {
		t.Fatal(err)
	}
	if grant, err := service.ConsumeGrant(context.Background(), token, "owner@example.com", "provision-operation-2"); err != nil || grant.Status != "consumed" {
		t.Fatalf("consume = %#v, err=%v", grant, err)
	}
	if _, err := service.ValidateGrant(context.Background(), token, "owner@example.com"); !errors.Is(err, ErrInvalidGrant) {
		t.Fatalf("consumed grant validated: %v", err)
	}
}

func TestInvalidAndExpiredReviewCapabilitiesFailClosed(t *testing.T) {
	repo, reviewToken, now := newApprovalFixture(t)
	service := NewService(repo, ServiceOptions{Now: func() time.Time { return now }})
	if _, err := service.Resolve(context.Background(), "invalid"); !errors.Is(err, ErrInvalidReview) {
		t.Fatalf("invalid error = %v", err)
	}
	repo.application.ReviewExpiresAt = now
	if _, err := service.Resolve(context.Background(), reviewToken); !errors.Is(err, ErrReviewExpired) {
		t.Fatalf("expired error = %v", err)
	}
}

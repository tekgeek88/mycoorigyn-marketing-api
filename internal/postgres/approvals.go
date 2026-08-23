package postgres

import (
	"context"
	"crypto/subtle"
	"database/sql"
	"errors"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/approvals"
)

func (s Store) ResolveReview(ctx context.Context, reviewDigest []byte, now time.Time) (approvals.Application, error) {
	application, reviewStatus, err := scanReview(s.pool.QueryRow(ctx, reviewQuery, reviewDigest))
	if errors.Is(err, pgx.ErrNoRows) {
		return approvals.Application{}, approvals.ErrInvalidReview
	}
	if err != nil {
		return approvals.Application{}, err
	}
	if !now.Before(application.ReviewExpiresAt) {
		return approvals.Application{}, approvals.ErrReviewExpired
	}
	if reviewStatus != "active" || application.ApprovalStatus != "pending" {
		return approvals.Application{}, approvals.ErrInvalidReview
	}
	return application, nil
}

func (s Store) Approve(ctx context.Context, reviewDigest []byte, candidate approvals.GrantCandidate, now time.Time) (approvals.Approval, bool, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return approvals.Approval{}, false, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	application, reviewStatus, err := scanReview(tx.QueryRow(ctx, reviewQuery+" FOR UPDATE OF r, s", reviewDigest))
	if errors.Is(err, pgx.ErrNoRows) {
		return approvals.Approval{}, false, approvals.ErrInvalidReview
	}
	if err != nil {
		return approvals.Approval{}, false, err
	}
	if !now.Before(application.ReviewExpiresAt) || reviewStatus == "expired" {
		return approvals.Approval{}, false, approvals.ErrReviewExpired
	}
	if reviewStatus == "revoked" {
		return approvals.Approval{}, false, approvals.ErrInvalidReview
	}

	if application.ApprovalStatus == "approved" && reviewStatus == "decided" {
		approval, err := scanApproval(tx.QueryRow(ctx, approvalQuery, application.ID))
		if err != nil {
			return approvals.Approval{}, false, err
		}
		if err := tx.Commit(ctx); err != nil {
			return approvals.Approval{}, false, err
		}
		return approval, false, nil
	}
	if application.ApprovalStatus != "pending" || reviewStatus != "active" {
		return approvals.Approval{}, false, approvals.ErrDecisionConflict
	}
	if len(candidate.TokenDigest) != 32 || candidate.SecretReference == "" || !candidate.ExpiresAt.After(now) {
		return approvals.Approval{}, false, approvals.ErrInvalidInput
	}

	if _, err := tx.Exec(ctx, `
		UPDATE early_access_submissions
		SET approval_status = 'approved', reviewed_at = $2, approved_at = $2
		WHERE id = $1::uuid
	`, application.ID, now); err != nil {
		return approvals.Approval{}, false, err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE early_access_review_capabilities
		SET status = 'decided', decided_at = $2
		WHERE early_access_submission_id = $1::uuid
	`, application.ID, now); err != nil {
		return approvals.Approval{}, false, err
	}

	var approval approvals.Approval
	approval.ApplicationID = application.ID
	approval.ApprovedEmail = application.Email
	approval.FarmName = application.FarmName
	approval.TokenDigest = append([]byte(nil), candidate.TokenDigest...)
	approval.SecretReference = candidate.SecretReference
	approval.ExpiresAt = candidate.ExpiresAt
	if err := tx.QueryRow(ctx, `
		INSERT INTO signup_grants (
			early_access_submission_id, approved_email, token_digest, secret_reference, expires_at
		) VALUES ($1::uuid, $2, $3, $4, $5)
		RETURNING id::text
	`, application.ID, application.Email, candidate.TokenDigest, candidate.SecretReference, candidate.ExpiresAt).Scan(&approval.GrantID); err != nil {
		return approvals.Approval{}, false, err
	}
	if err := tx.Commit(ctx); err != nil {
		return approvals.Approval{}, false, err
	}
	return approval, true, nil
}

func (s Store) Decline(ctx context.Context, reviewDigest []byte, now time.Time) error {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	application, reviewStatus, err := scanReview(tx.QueryRow(ctx, reviewQuery+" FOR UPDATE OF r, s", reviewDigest))
	if errors.Is(err, pgx.ErrNoRows) {
		return approvals.ErrInvalidReview
	}
	if err != nil {
		return err
	}
	if !now.Before(application.ReviewExpiresAt) || reviewStatus == "expired" {
		return approvals.ErrReviewExpired
	}
	if application.ApprovalStatus == "declined" && reviewStatus == "decided" {
		return tx.Commit(ctx)
	}
	if application.ApprovalStatus != "pending" || reviewStatus != "active" {
		return approvals.ErrDecisionConflict
	}
	if _, err := tx.Exec(ctx, `
		UPDATE early_access_submissions
		SET approval_status = 'declined', reviewed_at = $2, declined_at = $2
		WHERE id = $1::uuid
	`, application.ID, now); err != nil {
		return err
	}
	if _, err := tx.Exec(ctx, `
		UPDATE early_access_review_capabilities
		SET status = 'decided', decided_at = $2
		WHERE early_access_submission_id = $1::uuid
	`, application.ID, now); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (s Store) ValidateGrant(ctx context.Context, grantDigest []byte, email string, now time.Time) (approvals.Grant, error) {
	record, _, err := scanGrant(s.pool.QueryRow(ctx, grantQuery, grantDigest, email))
	if errors.Is(err, pgx.ErrNoRows) {
		return approvals.Grant{}, approvals.ErrInvalidGrant
	}
	if err != nil {
		return approvals.Grant{}, err
	}
	if !now.Before(record.ExpiresAt) || record.Status == "expired" {
		return approvals.Grant{}, approvals.ErrGrantExpired
	}
	if record.Status != "active" && record.Status != "claimed" {
		return approvals.Grant{}, approvals.ErrInvalidGrant
	}
	return record, nil
}

func (s Store) ClaimGrant(ctx context.Context, grantDigest []byte, email string, claimDigest []byte, now, claimExpiresAt time.Time) (approvals.Grant, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return approvals.Grant{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, currentClaim, err := scanGrant(tx.QueryRow(ctx, grantQuery+" FOR UPDATE", grantDigest, email))
	if errors.Is(err, pgx.ErrNoRows) {
		return approvals.Grant{}, approvals.ErrInvalidGrant
	}
	if err != nil {
		return approvals.Grant{}, err
	}
	if !now.Before(record.ExpiresAt) || record.Status == "expired" {
		return approvals.Grant{}, approvals.ErrGrantExpired
	}
	if record.Status == "claimed" {
		sameClaim := subtle.ConstantTimeCompare(currentClaim, claimDigest) == 1
		if !sameClaim && now.Before(record.ClaimExpiresAt) {
			return approvals.Grant{}, approvals.ErrGrantClaimed
		}
		if sameClaim && now.Before(record.ClaimExpiresAt) {
			if err := tx.Commit(ctx); err != nil {
				return approvals.Grant{}, err
			}
			return record, nil
		}
	} else if record.Status != "active" {
		return approvals.Grant{}, approvals.ErrInvalidGrant
	}
	if err := tx.QueryRow(ctx, `
		UPDATE signup_grants
		SET status = 'claimed', claimed_at = $2, claim_expires_at = $3, claim_reference_digest = $4
		WHERE id = $1::uuid
		RETURNING claim_expires_at
	`, record.ID, now, claimExpiresAt, claimDigest).Scan(&record.ClaimExpiresAt); err != nil {
		return approvals.Grant{}, err
	}
	record.Status = "claimed"
	if err := tx.Commit(ctx); err != nil {
		return approvals.Grant{}, err
	}
	return record, nil
}

func (s Store) ConsumeGrant(ctx context.Context, grantDigest []byte, email string, claimDigest []byte, now time.Time) (approvals.Grant, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return approvals.Grant{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, currentClaim, err := scanGrant(tx.QueryRow(ctx, grantQuery+" FOR UPDATE", grantDigest, email))
	if errors.Is(err, pgx.ErrNoRows) {
		return approvals.Grant{}, approvals.ErrInvalidGrant
	}
	if err != nil {
		return approvals.Grant{}, err
	}
	if record.Status == "consumed" {
		if subtle.ConstantTimeCompare(currentClaim, claimDigest) != 1 {
			return approvals.Grant{}, approvals.ErrInvalidClaim
		}
		if err := tx.Commit(ctx); err != nil {
			return approvals.Grant{}, err
		}
		return record, nil
	}
	if record.Status != "claimed" || !now.Before(record.ExpiresAt) || !now.Before(record.ClaimExpiresAt) || subtle.ConstantTimeCompare(currentClaim, claimDigest) != 1 {
		return approvals.Grant{}, approvals.ErrInvalidClaim
	}
	if _, err := tx.Exec(ctx, `
		UPDATE signup_grants
		SET status = 'consumed', consumed_at = $2,
			claimed_at = NULL, claim_expires_at = NULL
		WHERE id = $1::uuid
	`, record.ID, now); err != nil {
		return approvals.Grant{}, err
	}
	record.Status = "consumed"
	record.ClaimExpiresAt = time.Time{}
	if err := tx.Commit(ctx); err != nil {
		return approvals.Grant{}, err
	}
	return record, nil
}

func (s Store) ReleaseGrant(ctx context.Context, grantDigest []byte, email string, claimDigest []byte, now time.Time) (approvals.Grant, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return approvals.Grant{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()
	record, currentClaim, err := scanGrant(tx.QueryRow(ctx, grantQuery+" FOR UPDATE", grantDigest, email))
	if errors.Is(err, pgx.ErrNoRows) {
		return approvals.Grant{}, approvals.ErrInvalidGrant
	}
	if err != nil {
		return approvals.Grant{}, err
	}
	if record.Status != "claimed" || subtle.ConstantTimeCompare(currentClaim, claimDigest) != 1 {
		return approvals.Grant{}, approvals.ErrInvalidClaim
	}
	if !now.Before(record.ExpiresAt) {
		return approvals.Grant{}, approvals.ErrGrantExpired
	}
	if _, err := tx.Exec(ctx, `
		UPDATE signup_grants
		SET status = 'active', claimed_at = NULL, claim_expires_at = NULL, claim_reference_digest = NULL
		WHERE id = $1::uuid
	`, record.ID); err != nil {
		return approvals.Grant{}, err
	}
	record.Status = "active"
	record.ClaimExpiresAt = time.Time{}
	if err := tx.Commit(ctx); err != nil {
		return approvals.Grant{}, err
	}
	return record, nil
}

const reviewQuery = `
	SELECT
		s.id::text,
		COALESCE(s.name, ''),
		s.email,
		COALESCE(s.farm_name, ''),
		COALESCE(s.farm_type, ''),
		COALESCE(s.production_scale, ''),
		COALESCE(s.current_tracking_method, ''),
		COALESCE(s.features_of_interest, ARRAY[]::text[]),
		s.interested_in_testing,
		COALESCE(s.message, ''),
		s.source,
		s.approval_status,
		r.expires_at,
		r.status
	FROM early_access_review_capabilities r
	JOIN early_access_submissions s ON s.id = r.early_access_submission_id
	WHERE r.token_digest = $1
`

type rowScanner interface {
	Scan(dest ...any) error
}

func scanReview(row rowScanner) (approvals.Application, string, error) {
	var app approvals.Application
	var interested sql.NullBool
	var reviewStatus string
	err := row.Scan(
		&app.ID, &app.Name, &app.Email, &app.FarmName, &app.FarmType,
		&app.ProductionScale, &app.CurrentTrackingMethod, &app.FeaturesOfInterest,
		&interested, &app.Message, &app.Source, &app.ApprovalStatus,
		&app.ReviewExpiresAt, &reviewStatus,
	)
	if interested.Valid {
		app.InterestedInTesting = &interested.Bool
	}
	return app, reviewStatus, err
}

const approvalQuery = `
	SELECT g.id::text, s.id::text, g.approved_email, COALESCE(s.farm_name, ''),
		g.token_digest, g.secret_reference, g.expires_at
	FROM signup_grants g
	JOIN early_access_submissions s ON s.id = g.early_access_submission_id
	WHERE g.early_access_submission_id = $1::uuid
`

func scanApproval(row rowScanner) (approvals.Approval, error) {
	var approval approvals.Approval
	err := row.Scan(&approval.GrantID, &approval.ApplicationID, &approval.ApprovedEmail, &approval.FarmName, &approval.TokenDigest, &approval.SecretReference, &approval.ExpiresAt)
	return approval, err
}

const grantQuery = `
	SELECT id::text, approved_email, source, status, expires_at,
		claim_expires_at,
		COALESCE(claim_reference_digest, ''::bytea), secret_reference
	FROM signup_grants
	WHERE token_digest = $1 AND approved_email = $2
`

func scanGrant(row rowScanner) (approvals.Grant, []byte, error) {
	var grant approvals.Grant
	var claimDigest []byte
	var claimExpiresAt sql.NullTime
	err := row.Scan(&grant.ID, &grant.ApprovedEmail, &grant.Source, &grant.Status, &grant.ExpiresAt, &claimExpiresAt, &claimDigest, &grant.SecretReference)
	if claimExpiresAt.Valid {
		grant.ClaimExpiresAt = claimExpiresAt.Time
	}
	return grant, claimDigest, err
}

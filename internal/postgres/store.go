package postgres

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/pageviews"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/submissions"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) Store {
	return Store{pool: pool}
}

func (s Store) CreateSubmission(ctx context.Context, submission submissions.Submission, duplicateSince time.Time, review *submissions.ReviewCapability) (submissions.CreateResult, error) {
	if err := submissions.ValidateStoredStatus(submission.Status); err != nil {
		return submissions.CreateResult{}, err
	}
	if (submission.SubmissionType == submissions.TypeEarlyAccess) != (review != nil) {
		return submissions.CreateResult{}, errors.New("review capability must exist only for early-access submissions")
	}

	var features []string
	if len(submission.FeaturesOfInterest) > 0 {
		features = append([]string(nil), submission.FeaturesOfInterest...)
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return submissions.CreateResult{}, err
	}
	defer func() { _ = tx.Rollback(ctx) }()

	if _, err := tx.Exec(ctx, `SELECT pg_advisory_xact_lock(hashtextextended($1, 0))`, submission.PayloadFingerprint); err != nil {
		return submissions.CreateResult{}, err
	}

	var existingID string
	err = tx.QueryRow(ctx, `
		SELECT id::text
		FROM early_access_submissions
		WHERE payload_fingerprint = $1
		  AND created_at >= $2
		ORDER BY created_at DESC
		LIMIT 1
	`, submission.PayloadFingerprint, duplicateSince).Scan(&existingID)
	if err == nil {
		if err := tx.Commit(ctx); err != nil {
			return submissions.CreateResult{}, err
		}
		return submissions.CreateResult{SubmissionID: existingID, Created: false}, nil
	}
	if !errors.Is(err, pgx.ErrNoRows) {
		return submissions.CreateResult{}, err
	}

	var approvalStatus any
	if submission.SubmissionType == submissions.TypeEarlyAccess {
		approvalStatus = "pending"
	}
	var submissionID string
	err = tx.QueryRow(ctx, `
		INSERT INTO early_access_submissions (
			submission_type,
			email,
			name,
			farm_name,
			farm_type,
			production_scale,
			current_tracking_method,
			features_of_interest,
			interested_in_testing,
			message,
			source,
			status,
			payload_fingerprint,
			approval_status
		) VALUES (
			$1,
			$2,
			$3,
			$4,
			$5,
			$6,
			$7,
			$8,
			$9,
			$10,
			$11,
			$12,
			$13,
			$14
		)
		RETURNING id::text
	`,
		submission.SubmissionType,
		submission.Email,
		nullIfEmpty(submission.Name),
		nullIfEmpty(submission.FarmName),
		nullIfEmpty(submission.FarmType),
		nullIfEmpty(submission.ProductionScale),
		nullIfEmpty(submission.CurrentTrackingMethod),
		features,
		submission.InterestedInTesting,
		nullIfEmpty(submission.Message),
		submission.Source,
		submission.Status,
		submission.PayloadFingerprint,
		approvalStatus,
	).Scan(&submissionID)
	if err != nil {
		return submissions.CreateResult{}, err
	}

	if review != nil {
		if _, err := tx.Exec(ctx, `
			INSERT INTO early_access_review_capabilities (
				early_access_submission_id,
				token_digest,
				secret_reference,
				expires_at
			) VALUES ($1::uuid, $2, $3, $4)
		`, submissionID, review.TokenDigest, review.SecretReference, review.ExpiresAt); err != nil {
			return submissions.CreateResult{}, err
		}
	}

	if err := tx.Commit(ctx); err != nil {
		return submissions.CreateResult{}, err
	}
	return submissions.CreateResult{SubmissionID: submissionID, Created: true}, nil
}

func (s Store) RecordVisit(ctx context.Context, pagePath string, visitorID string) (pageviews.VisitorCounter, error) {
	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return pageviews.VisitorCounter{}, err
	}
	defer func() {
		_ = tx.Rollback(ctx)
	}()

	if _, err := tx.Exec(ctx, `
		INSERT INTO page_view_counters (page_path)
		VALUES ($1)
		ON CONFLICT (page_path) DO NOTHING
	`, pagePath); err != nil {
		return pageviews.VisitorCounter{}, err
	}

	isNewVisitor := false
	if visitorID != "" {
		commandTag, err := tx.Exec(ctx, `
			INSERT INTO page_view_visitors (page_path, visitor_id)
			VALUES ($1, $2)
			ON CONFLICT (page_path, visitor_id) DO NOTHING
		`, pagePath, visitorID)
		if err != nil {
			return pageviews.VisitorCounter{}, err
		}
		isNewVisitor = commandTag.RowsAffected() > 0
	}

	if isNewVisitor {
		if _, err := tx.Exec(ctx, `
			UPDATE page_view_counters
			SET total_visits = total_visits + 1,
				unique_visitors = unique_visitors + 1,
				updated_at = now()
			WHERE page_path = $1
		`, pagePath); err != nil {
			return pageviews.VisitorCounter{}, err
		}
	} else {
		if _, err := tx.Exec(ctx, `
			UPDATE page_view_counters
			SET total_visits = total_visits + 1,
				updated_at = now()
			WHERE page_path = $1
		`, pagePath); err != nil {
			return pageviews.VisitorCounter{}, err
		}
	}

	var counter pageviews.VisitorCounter
	counter.Page = pagePath
	if err := tx.QueryRow(ctx, `
		SELECT total_visits, unique_visitors
		FROM page_view_counters
		WHERE page_path = $1
	`, pagePath).Scan(&counter.TotalVisits, &counter.UniqueVisitors); err != nil {
		return pageviews.VisitorCounter{}, err
	}

	if err := tx.Commit(ctx); err != nil {
		return pageviews.VisitorCounter{}, err
	}

	return counter, nil
}

func (s Store) GetCounts(ctx context.Context, pagePath string) (pageviews.VisitorCounter, error) {
	var counter pageviews.VisitorCounter
	counter.Page = pagePath

	row := s.pool.QueryRow(ctx, `
		SELECT total_visits, unique_visitors
		FROM page_view_counters
		WHERE page_path = $1
	`, pagePath)

	var err error
	err = row.Scan(&counter.TotalVisits, &counter.UniqueVisitors)
	if err != nil {
		if err == pgx.ErrNoRows {
			return counter, nil
		}
		return pageviews.VisitorCounter{}, err
	}

	return counter, nil
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

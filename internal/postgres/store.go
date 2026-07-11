package postgres

import (
	"context"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/submissions"
)

type Store struct {
	pool *pgxpool.Pool
}

func NewStore(pool *pgxpool.Pool) Store {
	return Store{pool: pool}
}

func (s Store) HasRecentFingerprint(ctx context.Context, fingerprint string, since time.Time) (bool, error) {
	var exists bool
	err := s.pool.QueryRow(ctx, `
		SELECT EXISTS (
			SELECT 1
			FROM early_access_submissions
			WHERE payload_fingerprint = $1
			  AND created_at >= $2
		)
	`, fingerprint, since).Scan(&exists)
	return exists, err
}

func (s Store) CreateEarlyAccessSubmission(ctx context.Context, submission submissions.Submission) error {
	if err := submissions.ValidateStoredStatus(submission.Status); err != nil {
		return err
	}

	var features []string
	if len(submission.FeaturesOfInterest) > 0 {
		features = append([]string(nil), submission.FeaturesOfInterest...)
	}

	_, err := s.pool.Exec(ctx, `
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
			payload_fingerprint
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
			$13
		)
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
	)
	return err
}

func nullIfEmpty(value string) any {
	if strings.TrimSpace(value) == "" {
		return nil
	}
	return value
}

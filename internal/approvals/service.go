package approvals

import (
	"context"
	"crypto/subtle"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"strings"
	"time"

	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/securetokens"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/transactionalemail"
)

var (
	ErrInvalidReview    = errors.New("review capability is invalid")
	ErrReviewExpired    = errors.New("review capability has expired")
	ErrDecisionConflict = errors.New("review decision conflicts with durable state")
	ErrDeliveryFailed   = errors.New("approval email delivery failed")
	ErrInvalidGrant     = errors.New("signup grant is invalid")
	ErrGrantExpired     = errors.New("signup grant has expired")
	ErrGrantClaimed     = errors.New("signup grant is claimed by another operation")
	ErrInvalidClaim     = errors.New("signup grant claim is invalid")
	ErrInvalidInput     = errors.New("request input is invalid")
)

const GrantSourceEarlyAccess = "early_access_approval"

type Repository interface {
	ResolveReview(ctx context.Context, reviewDigest []byte, now time.Time) (Application, error)
	Approve(ctx context.Context, reviewDigest []byte, candidate GrantCandidate, now time.Time) (Approval, bool, error)
	Decline(ctx context.Context, reviewDigest []byte, now time.Time) error
	ValidateGrant(ctx context.Context, grantDigest []byte, email string, now time.Time) (Grant, error)
	ClaimGrant(ctx context.Context, grantDigest []byte, email string, claimDigest []byte, now, claimExpiresAt time.Time) (Grant, error)
	ConsumeGrant(ctx context.Context, grantDigest []byte, email string, claimDigest []byte, now time.Time) (Grant, error)
	ReleaseGrant(ctx context.Context, grantDigest []byte, email string, claimDigest []byte, now time.Time) (Grant, error)
}

type Application struct {
	ID                    string    `json:"-"`
	Name                  string    `json:"name"`
	Email                 string    `json:"email"`
	FarmName              string    `json:"farm_name"`
	FarmType              string    `json:"farm_type"`
	ProductionScale       string    `json:"production_scale"`
	CurrentTrackingMethod string    `json:"current_tracking_method"`
	FeaturesOfInterest    []string  `json:"features_of_interest"`
	InterestedInTesting   *bool     `json:"interested_in_testing"`
	Message               string    `json:"message"`
	Source                string    `json:"source"`
	ApprovalStatus        string    `json:"approval_status"`
	ReviewExpiresAt       time.Time `json:"review_expires_at"`
}

type GrantCandidate struct {
	TokenDigest     []byte
	SecretReference string
	ExpiresAt       time.Time
}

type Approval struct {
	ApplicationID   string
	GrantID         string
	ApprovedEmail   string
	FarmName        string
	TokenDigest     []byte
	SecretReference string
	ExpiresAt       time.Time
}

type ApprovalResult struct {
	Status         string `json:"status"`
	DeliveryStatus string `json:"delivery_status"`
}

type Grant struct {
	ID              string    `json:"grant_id"`
	ApprovedEmail   string    `json:"approved_email"`
	Source          string    `json:"source"`
	Status          string    `json:"status"`
	ExpiresAt       time.Time `json:"expires_at"`
	ClaimExpiresAt  time.Time `json:"claim_expires_at,omitempty"`
	SecretReference string    `json:"-"`
}

type Service struct {
	repo          Repository
	tokens        securetokens.Store
	email         transactionalemail.Sender
	from          string
	replyTo       string
	signupBaseURL string
	now           func() time.Time
	grantLifetime time.Duration
	claimLifetime time.Duration
	logger        *slog.Logger
}

type ServiceOptions struct {
	Tokens        securetokens.Store
	Email         transactionalemail.Sender
	From          string
	ReplyTo       string
	SignupBaseURL string
	Now           func() time.Time
	GrantLifetime time.Duration
	ClaimLifetime time.Duration
	Logger        *slog.Logger
}

func NewService(repo Repository, opts ServiceOptions) Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	grantLifetime := opts.GrantLifetime
	if grantLifetime <= 0 {
		grantLifetime = 7 * 24 * time.Hour
	}
	claimLifetime := opts.ClaimLifetime
	if claimLifetime <= 0 {
		claimLifetime = 30 * time.Minute
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(discardWriter{}, nil))
	}
	return Service{
		repo: repo, tokens: opts.Tokens, email: opts.Email,
		from: strings.TrimSpace(opts.From), replyTo: strings.TrimSpace(opts.ReplyTo),
		signupBaseURL: strings.TrimRight(strings.TrimSpace(opts.SignupBaseURL), "/"),
		now:           now, grantLifetime: grantLifetime, claimLifetime: claimLifetime, logger: logger,
	}
}

func (s Service) Resolve(ctx context.Context, token string) (Application, error) {
	if err := validateToken(token); err != nil {
		return Application{}, ErrInvalidReview
	}
	return s.repo.ResolveReview(ctx, securetokens.Digest(token), s.now())
}

func (s Service) Approve(ctx context.Context, reviewToken string) (ApprovalResult, error) {
	if err := validateToken(reviewToken); err != nil || s.tokens == nil {
		return ApprovalResult{}, ErrInvalidReview
	}
	grantToken, grantDigest, err := securetokens.Generate()
	if err != nil {
		return ApprovalResult{}, err
	}
	reference, err := s.tokens.Put(ctx, "signup", grantDigest, grantToken)
	if err != nil {
		return ApprovalResult{}, err
	}
	candidate := GrantCandidate{TokenDigest: grantDigest, SecretReference: reference, ExpiresAt: s.now().Add(s.grantLifetime)}
	approval, usedCandidate, err := s.repo.Approve(ctx, securetokens.Digest(reviewToken), candidate, s.now())
	if err != nil {
		_ = s.tokens.Delete(ctx, reference)
		return ApprovalResult{}, err
	}
	if !usedCandidate {
		_ = s.tokens.Delete(ctx, reference)
		grantToken, err = s.tokens.Read(ctx, approval.SecretReference)
		if err != nil {
			return ApprovalResult{}, errors.New("approved signup grant token is unavailable")
		}
	}
	if subtle.ConstantTimeCompare(securetokens.Digest(grantToken), approval.TokenDigest) != 1 {
		return ApprovalResult{}, errors.New("approved signup grant token failed integrity validation")
	}
	if err := s.sendApproval(ctx, approval, grantToken); err != nil {
		s.logger.Warn("early_access_approval_delivery_failed", "submission_id", approval.ApplicationID, "grant_id", approval.GrantID, "outcome", "delivery_failed")
		return ApprovalResult{Status: "approved", DeliveryStatus: "failed"}, ErrDeliveryFailed
	}
	s.logger.Info("early_access_approval_delivered", "submission_id", approval.ApplicationID, "grant_id", approval.GrantID, "outcome", "delivered")
	return ApprovalResult{Status: "approved", DeliveryStatus: "delivered"}, nil
}

func (s Service) Decline(ctx context.Context, reviewToken string) error {
	if err := validateToken(reviewToken); err != nil {
		return ErrInvalidReview
	}
	return s.repo.Decline(ctx, securetokens.Digest(reviewToken), s.now())
}

func (s Service) ValidateGrant(ctx context.Context, token, email string) (Grant, error) {
	email, err := normalizeEmail(email)
	if err != nil || validateToken(token) != nil {
		return Grant{}, ErrInvalidGrant
	}
	return s.repo.ValidateGrant(ctx, securetokens.Digest(token), email, s.now())
}

func (s Service) ClaimGrant(ctx context.Context, token, email, claimReference string) (Grant, error) {
	email, claimDigest, err := normalizeGrantOperation(token, email, claimReference)
	if err != nil {
		return Grant{}, err
	}
	now := s.now()
	return s.repo.ClaimGrant(ctx, securetokens.Digest(token), email, claimDigest, now, now.Add(s.claimLifetime))
}

func (s Service) ConsumeGrant(ctx context.Context, token, email, claimReference string) (Grant, error) {
	email, claimDigest, err := normalizeGrantOperation(token, email, claimReference)
	if err != nil {
		return Grant{}, err
	}
	grant, err := s.repo.ConsumeGrant(ctx, securetokens.Digest(token), email, claimDigest, s.now())
	if err == nil && s.tokens != nil && grant.SecretReference != "" {
		if cleanupErr := s.tokens.Delete(ctx, grant.SecretReference); cleanupErr != nil {
			s.logger.Warn("signup_grant_token_cleanup_failed", "grant_id", grant.ID, "outcome", "cleanup_failed")
		}
	}
	return grant, err
}

func (s Service) ReleaseGrant(ctx context.Context, token, email, claimReference string) (Grant, error) {
	email, claimDigest, err := normalizeGrantOperation(token, email, claimReference)
	if err != nil {
		return Grant{}, err
	}
	return s.repo.ReleaseGrant(ctx, securetokens.Digest(token), email, claimDigest, s.now())
}

func normalizeGrantOperation(token, email, claimReference string) (string, []byte, error) {
	email, err := normalizeEmail(email)
	if err != nil || validateToken(token) != nil {
		return "", nil, ErrInvalidGrant
	}
	claimReference = strings.TrimSpace(claimReference)
	if len(claimReference) < 16 || len(claimReference) > 200 {
		return "", nil, ErrInvalidClaim
	}
	return email, securetokens.Digest(claimReference), nil
}

func normalizeEmail(value string) (string, error) {
	value = strings.ToLower(strings.TrimSpace(value))
	address, err := mail.ParseAddress(value)
	if err != nil || address.Address != value || len(value) > 320 {
		return "", ErrInvalidInput
	}
	return value, nil
}

func validateToken(token string) error {
	token = strings.TrimSpace(token)
	if len(token) != 43 || strings.ContainsAny(token, "\r\n ") {
		return ErrInvalidInput
	}
	return nil
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func (s Service) sendApproval(ctx context.Context, approval Approval, token string) error {
	if s.email == nil || s.from == "" || s.signupBaseURL == "" {
		return errors.New("approval delivery is not configured")
	}
	message, err := buildApprovalMessage(s.from, s.replyTo, s.signupBaseURL, approval, token)
	if err != nil {
		return fmt.Errorf("render approval email: %w", err)
	}
	if err := s.email.Send(ctx, message); err != nil {
		return fmt.Errorf("send approval email: %w", err)
	}
	return nil
}

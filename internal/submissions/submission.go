package submissions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/mail"
	"net/url"
	"sort"
	"strings"
	"time"

	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/securetokens"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/transactionalemail"
)

const (
	TypeWaitlist    = "waitlist"
	TypeEarlyAccess = "early_access"

	DefaultSource = "marketing_site"
	DefaultStatus = "new"
)

const (
	MaxRequestBodyBytes            int64 = 32 * 1024
	MaxEmailLength                       = 320
	MaxNameLength                        = 120
	MaxFarmNameLength                    = 160
	MaxFarmTypeLength                    = 120
	MaxProductionScaleLength             = 120
	MaxCurrentTrackingMethodLength       = 240
	MaxFeatureLength                     = 80
	MaxFeatureCount                      = 20
	MaxMessageLength                     = 2000
	MaxSourceLength                      = 80
)

var allowedStatuses = map[string]struct{}{
	DefaultStatus: {},
	"contacted":   {},
	"testing":     {},
	"closed":      {},
}

type Repository interface {
	CreateSubmission(ctx context.Context, submission Submission, duplicateSince time.Time, review *ReviewCapability) (CreateResult, error)
}

type Service struct {
	repo            Repository
	now             func() time.Time
	duplicateWindow time.Duration
	reviewLifetime  time.Duration
	tokens          securetokens.Store
	email           transactionalemail.Sender
	emailFrom       string
	emailReplyTo    string
	reviewerEmail   string
	reviewBaseURL   string
	logger          *slog.Logger
}

type ServiceOptions struct {
	Now             func() time.Time
	DuplicateWindow time.Duration
	ReviewLifetime  time.Duration
	Tokens          securetokens.Store
	Email           transactionalemail.Sender
	EmailFrom       string
	EmailReplyTo    string
	ReviewerEmail   string
	ReviewBaseURL   string
	Logger          *slog.Logger
}

type ReviewCapability struct {
	TokenDigest     []byte
	SecretReference string
	ExpiresAt       time.Time
}

type CreateResult struct {
	SubmissionID string
	Created      bool
}

type SubmissionInput struct {
	SubmissionType        string
	Email                 string
	Name                  string
	FarmName              string
	FarmType              string
	ProductionScale       string
	CurrentTrackingMethod string
	FeaturesOfInterest    []string
	InterestedInTesting   *bool
	Message               string
	Source                string
	Website               string
}

type Submission struct {
	SubmissionType        string
	Email                 string
	Name                  string
	FarmName              string
	FarmType              string
	ProductionScale       string
	CurrentTrackingMethod string
	FeaturesOfInterest    []string
	InterestedInTesting   *bool
	Message               string
	Source                string
	Status                string
	PayloadFingerprint    string
}

type ValidationError struct {
	Message string
}

func (e ValidationError) Error() string {
	if e.Message == "" {
		return "validation error"
	}
	return e.Message
}

func NewService(repo Repository, opts ServiceOptions) Service {
	now := opts.Now
	if now == nil {
		now = time.Now
	}
	window := opts.DuplicateWindow
	if window <= 0 {
		window = 24 * time.Hour
	}
	reviewLifetime := opts.ReviewLifetime
	if reviewLifetime <= 0 {
		reviewLifetime = 7 * 24 * time.Hour
	}
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewTextHandler(ioDiscard{}, nil))
	}
	return Service{
		repo:            repo,
		now:             now,
		duplicateWindow: window,
		reviewLifetime:  reviewLifetime,
		tokens:          opts.Tokens,
		email:           opts.Email,
		emailFrom:       strings.TrimSpace(opts.EmailFrom),
		emailReplyTo:    strings.TrimSpace(opts.EmailReplyTo),
		reviewerEmail:   strings.TrimSpace(opts.ReviewerEmail),
		reviewBaseURL:   strings.TrimRight(strings.TrimSpace(opts.ReviewBaseURL), "/"),
		logger:          logger,
	}
}

func (s Service) Submit(ctx context.Context, input SubmissionInput) error {
	submission, err := Normalize(input)
	if err != nil {
		return err
	}

	var review *ReviewCapability
	var reviewToken string
	if submission.SubmissionType == TypeEarlyAccess {
		if s.tokens == nil {
			return errors.New("protected review token store is unavailable")
		}
		token, digest, err := securetokens.Generate()
		if err != nil {
			return err
		}
		reference, err := s.tokens.Put(ctx, "review", digest, token)
		if err != nil {
			return err
		}
		reviewToken = token
		review = &ReviewCapability{
			TokenDigest:     digest,
			SecretReference: reference,
			ExpiresAt:       s.now().Add(s.reviewLifetime),
		}
	}

	result, err := s.repo.CreateSubmission(ctx, submission, s.now().Add(-s.duplicateWindow), review)
	if err != nil {
		if review != nil {
			_ = s.tokens.Delete(ctx, review.SecretReference)
		}
		return err
	}
	if !result.Created {
		if review != nil {
			_ = s.tokens.Delete(ctx, review.SecretReference)
		}
		return nil
	}
	if review == nil {
		return nil
	}

	if err := s.sendReviewerNotification(ctx, submission, reviewToken, review.ExpiresAt); err != nil {
		s.logger.Warn("early_access_review_notification_failed",
			"submission_id", result.SubmissionID,
			"outcome", "delivery_failed",
		)
	}
	return nil
}

func (s Service) sendReviewerNotification(ctx context.Context, submission Submission, token string, expiresAt time.Time) error {
	if s.email == nil || s.emailFrom == "" || s.reviewerEmail == "" || s.reviewBaseURL == "" {
		return errors.New("review notification is not configured")
	}
	reviewURL := buildReviewURL(s.reviewBaseURL, token)
	expires := expiresAt.UTC().Format("January 2, 2006 at 15:04 UTC")
	contextName := fallback(submission.FarmName)
	if contextName == "—" {
		contextName = fallback(submission.Name)
	}
	if contextName == "—" {
		contextName = submission.Email
	}
	textBody := fmt.Sprintf(
		"New MycoOrigyn Early Access request — %s\n\nName: %s\nEmail: %s\nFarm: %s\nFarm type: %s\nProduction scale: %s\nCurrent tracking method: %s\nInterested in testing: %s\nMessage: %s\n\nReview application:\n%s\n\nReview link expires: %s.\n",
		contextName,
		fallback(submission.Name), submission.Email, fallback(submission.FarmName), fallback(submission.FarmType),
		fallback(submission.ProductionScale), fallback(submission.CurrentTrackingMethod), boolLabel(submission.InterestedInTesting),
		fallback(submission.Message), reviewURL, expires,
	)
	htmlBody, err := transactionalemail.RenderBrandedHTML(transactionalemail.BrandedContent{
		Preheader: "A new MycoOrigyn Early Access request is ready for review.",
		Eyebrow:   "Internal review",
		Heading:   "New Early Access request",
		Intro:     "A new farm application is ready for review. The protected review link is available until the expiration shown below.",
		Details: []transactionalemail.BrandedDetail{
			{Label: "Name", Value: fallback(submission.Name)},
			{Label: "Email", Value: submission.Email},
			{Label: "Farm", Value: fallback(submission.FarmName)},
			{Label: "Farm type", Value: fallback(submission.FarmType)},
			{Label: "Production scale", Value: fallback(submission.ProductionScale)},
			{Label: "Current tracking method", Value: fallback(submission.CurrentTrackingMethod)},
			{Label: "Interested in testing", Value: boolLabel(submission.InterestedInTesting)},
			{Label: "Message", Value: fallback(submission.Message)},
			{Label: "Review link expires", Value: expires},
		},
		Action:       transactionalemail.BrandedAction{Label: "Review application", URL: reviewURL},
		SecurityNote: "This review link is protected and intended only for the MycoOrigyn review team.",
	})
	if err != nil {
		return fmt.Errorf("render review notification: %w", err)
	}
	return s.email.Send(ctx, transactionalemail.Message{
		To: s.reviewerEmail, From: s.emailFrom, ReplyTo: s.emailReplyTo,
		Subject: "New MycoOrigyn Early Access request — " + contextName, Text: textBody, HTML: htmlBody,
	})
}

func buildReviewURL(baseURL, token string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") + "#token=" + url.PathEscape(token)
}

func fallback(value string) string {
	if strings.TrimSpace(value) == "" {
		return "—"
	}
	return value
}

func boolLabel(value *bool) string {
	if value == nil {
		return "Not provided"
	}
	if *value {
		return "Yes"
	}
	return "No"
}

type ioDiscard struct{}

func (ioDiscard) Write(p []byte) (int, error) { return len(p), nil }

func Normalize(input SubmissionInput) (Submission, error) {
	input.SubmissionType = strings.TrimSpace(input.SubmissionType)
	if input.SubmissionType != TypeWaitlist && input.SubmissionType != TypeEarlyAccess {
		return Submission{}, ValidationError{Message: "submission_type must be waitlist or early_access"}
	}

	input.Email = strings.ToLower(strings.TrimSpace(input.Email))
	if !validEmail(input.Email) {
		return Submission{}, ValidationError{Message: "email must be valid"}
	}

	input.Name = normalizeOptional(input.Name)
	input.FarmName = normalizeOptional(input.FarmName)
	input.FarmType = normalizeOptional(input.FarmType)
	input.ProductionScale = normalizeOptional(input.ProductionScale)
	input.CurrentTrackingMethod = normalizeOptional(input.CurrentTrackingMethod)
	input.Message = normalizeOptional(input.Message)
	input.Source = normalizeOptional(input.Source)
	input.Website = strings.TrimSpace(input.Website)

	if input.Website != "" {
		return Submission{}, ValidationError{Message: "website honeypot must be empty"}
	}

	if input.Source == "" {
		input.Source = DefaultSource
	}

	if err := validateLength("email", input.Email, MaxEmailLength); err != nil {
		return Submission{}, err
	}
	if err := validateLength("name", input.Name, MaxNameLength); err != nil {
		return Submission{}, err
	}
	if err := validateLength("farm_name", input.FarmName, MaxFarmNameLength); err != nil {
		return Submission{}, err
	}
	if err := validateLength("farm_type", input.FarmType, MaxFarmTypeLength); err != nil {
		return Submission{}, err
	}
	if err := validateLength("production_scale", input.ProductionScale, MaxProductionScaleLength); err != nil {
		return Submission{}, err
	}
	if err := validateLength("current_tracking_method", input.CurrentTrackingMethod, MaxCurrentTrackingMethodLength); err != nil {
		return Submission{}, err
	}
	if err := validateLength("message", input.Message, MaxMessageLength); err != nil {
		return Submission{}, err
	}
	if err := validateLength("source", input.Source, MaxSourceLength); err != nil {
		return Submission{}, err
	}

	features, err := normalizeFeatures(input.FeaturesOfInterest)
	if err != nil {
		return Submission{}, err
	}

	submission := Submission{
		SubmissionType:        input.SubmissionType,
		Email:                 input.Email,
		Name:                  input.Name,
		FarmName:              input.FarmName,
		FarmType:              input.FarmType,
		ProductionScale:       input.ProductionScale,
		CurrentTrackingMethod: input.CurrentTrackingMethod,
		FeaturesOfInterest:    features,
		InterestedInTesting:   input.InterestedInTesting,
		Message:               input.Message,
		Source:                input.Source,
		Status:                DefaultStatus,
	}
	submission.PayloadFingerprint = buildPayloadFingerprint(submission)
	return submission, nil
}

func validateStatus(status string) error {
	if _, ok := allowedStatuses[status]; !ok {
		return fmt.Errorf("unsupported status %q", status)
	}
	return nil
}

func normalizeOptional(value string) string {
	return strings.TrimSpace(value)
}

func validateLength(field, value string, max int) error {
	if len(value) > max {
		return ValidationError{Message: fmt.Sprintf("%s exceeds the maximum length", field)}
	}
	return nil
}

func normalizeFeatures(features []string) ([]string, error) {
	if len(features) > MaxFeatureCount {
		return nil, ValidationError{Message: "features_of_interest exceeds the maximum number of entries"}
	}

	normalized := make([]string, 0, len(features))
	for _, feature := range features {
		feature = strings.TrimSpace(feature)
		if feature == "" {
			continue
		}
		if len(feature) > MaxFeatureLength {
			return nil, ValidationError{Message: "features_of_interest contains a value that exceeds the maximum length"}
		}
		normalized = append(normalized, feature)
	}
	return normalized, nil
}

func buildPayloadFingerprint(submission Submission) string {
	features := append([]string(nil), submission.FeaturesOfInterest...)
	sort.Strings(features)

	payload := struct {
		SubmissionType        string   `json:"submission_type"`
		Email                 string   `json:"email"`
		Name                  string   `json:"name,omitempty"`
		FarmName              string   `json:"farm_name,omitempty"`
		FarmType              string   `json:"farm_type,omitempty"`
		ProductionScale       string   `json:"production_scale,omitempty"`
		CurrentTrackingMethod string   `json:"current_tracking_method,omitempty"`
		FeaturesOfInterest    []string `json:"features_of_interest,omitempty"`
		InterestedInTesting   *bool    `json:"interested_in_testing,omitempty"`
		Message               string   `json:"message,omitempty"`
		Source                string   `json:"source"`
	}{
		SubmissionType:        submission.SubmissionType,
		Email:                 submission.Email,
		Name:                  submission.Name,
		FarmName:              submission.FarmName,
		FarmType:              submission.FarmType,
		ProductionScale:       submission.ProductionScale,
		CurrentTrackingMethod: submission.CurrentTrackingMethod,
		FeaturesOfInterest:    features,
		InterestedInTesting:   submission.InterestedInTesting,
		Message:               submission.Message,
		Source:                submission.Source,
	}

	body, _ := json.Marshal(payload)
	sum := sha256.Sum256(body)
	return hex.EncodeToString(sum[:])
}

func validEmail(email string) bool {
	if email == "" || len(email) > MaxEmailLength || strings.ContainsAny(email, " \t\r\n") {
		return false
	}

	address, err := mail.ParseAddress(email)
	if err != nil || address.Address != email {
		return false
	}

	local, domain, ok := strings.Cut(email, "@")
	if !ok || local == "" || domain == "" || strings.HasPrefix(domain, ".") || strings.HasSuffix(domain, ".") {
		return false
	}

	return true
}

func ValidateStoredStatus(status string) error {
	return validateStatus(status)
}

var ErrValidation = errors.New("validation error")

package submissions

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net/mail"
	"sort"
	"strings"
	"time"
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
	HasRecentFingerprint(ctx context.Context, fingerprint string, since time.Time) (bool, error)
	CreateEarlyAccessSubmission(ctx context.Context, submission Submission) error
}

type Service struct {
	repo            Repository
	now             func() time.Time
	duplicateWindow time.Duration
}

type ServiceOptions struct {
	Now             func() time.Time
	DuplicateWindow time.Duration
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
	return Service{
		repo:            repo,
		now:             now,
		duplicateWindow: window,
	}
}

func (s Service) Submit(ctx context.Context, input SubmissionInput) error {
	submission, err := Normalize(input)
	if err != nil {
		return err
	}

	duplicate, err := s.repo.HasRecentFingerprint(ctx, submission.PayloadFingerprint, s.now().Add(-s.duplicateWindow))
	if err != nil {
		return err
	}
	if duplicate {
		return nil
	}

	return s.repo.CreateEarlyAccessSubmission(ctx, submission)
}

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

package httpapi

import (
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/approvals"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/pageviews"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/submissions"
)

type Options struct {
	CORSAllowedOrigins []string
	Logger             *slog.Logger
	Approvals          *approvals.Service
	ProvisioningSecret []byte
}

func NewServer(service submissions.Service, pageViews pageviews.CounterService, opts Options) *gin.Engine {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	server := &server{
		service:            service,
		pageViews:          pageViews,
		approvals:          opts.Approvals,
		provisioningSecret: append([]byte(nil), opts.ProvisioningSecret...),
		logger:             logger,
		corsAllowedOrigins: map[string]struct{}{},
	}
	for _, origin := range opts.CORSAllowedOrigins {
		origin = strings.TrimSpace(origin)
		if origin != "" {
			server.corsAllowedOrigins[origin] = struct{}{}
		}
	}

	router := gin.New()
	router.HandleMethodNotAllowed = true
	router.Use(requestLogger(logger), gin.CustomRecoveryWithWriter(io.Discard, recoveryHandler(logger)))
	router.NoMethod(func(c *gin.Context) {
		writeError(c, http.StatusMethodNotAllowed, "method_not_allowed", "Please use a supported request method.")
	})

	router.GET("/healthz", server.healthz)

	public := router.Group("/public")
	public.Use(publicCORS(server.corsAllowedOrigins), limitRequestBody(submissions.MaxRequestBodyBytes))
	public.POST("/early-access", server.earlyAccess)
	public.OPTIONS("/early-access", server.earlyAccessOptions)
	public.POST("/early-access/review/resolve", server.resolveReview)
	public.POST("/early-access/review/approve", server.approveReview)
	public.POST("/early-access/review/decline", server.declineReview)
	public.OPTIONS("/early-access/review/resolve", server.publicOptions)
	public.OPTIONS("/early-access/review/approve", server.publicOptions)
	public.OPTIONS("/early-access/review/decline", server.publicOptions)
	public.POST("/visitor-count", server.recordVisitorCount)
	public.GET("/visitor-count", server.getVisitorCount)
	public.OPTIONS("/visitor-count", server.publicOptions)

	internal := router.Group("/internal")
	internal.Use(limitRequestBody(submissions.MaxRequestBodyBytes), requireServiceAuthentication(server.provisioningSecret))
	internal.POST("/signup-grants/validate", server.validateSignupGrant)
	internal.POST("/signup-grants/claim", server.claimSignupGrant)
	internal.POST("/signup-grants/consume", server.consumeSignupGrant)
	internal.POST("/signup-grants/release", server.releaseSignupGrant)

	return router
}

type server struct {
	service            submissions.Service
	pageViews          pageviews.CounterService
	approvals          *approvals.Service
	provisioningSecret []byte
	logger             *slog.Logger
	corsAllowedOrigins map[string]struct{}
}

type earlyAccessRequest struct {
	SubmissionType        string   `json:"submission_type"`
	Email                 string   `json:"email"`
	Name                  string   `json:"name"`
	FarmName              string   `json:"farm_name"`
	FarmType              string   `json:"farm_type"`
	ProductionScale       string   `json:"production_scale"`
	CurrentTrackingMethod string   `json:"current_tracking_method"`
	FeaturesOfInterest    []string `json:"features_of_interest"`
	InterestedInTesting   *bool    `json:"interested_in_testing"`
	Message               string   `json:"message"`
	Source                string   `json:"source"`
	Website               string   `json:"website"`
}

type visitorCountRequest struct {
	Page      string `json:"page"`
	VisitorID string `json:"visitor_id"`
}

type capabilityRequest struct {
	Token string `json:"token"`
}

type signupGrantRequest struct {
	Token          string `json:"token"`
	Email          string `json:"email"`
	ClaimReference string `json:"claim_reference,omitempty"`
}

func (s *server) healthz(c *gin.Context) {
	c.JSON(http.StatusOK, gin.H{"status": "ok"})
}

func (s *server) earlyAccess(c *gin.Context) {
	req, err := decodeEarlyAccessRequest(c)
	if err != nil {
		s.writeDecodeError(c, err)
		return
	}

	err = s.service.Submit(c.Request.Context(), submissions.SubmissionInput{
		SubmissionType:        req.SubmissionType,
		Email:                 req.Email,
		Name:                  req.Name,
		FarmName:              req.FarmName,
		FarmType:              req.FarmType,
		ProductionScale:       req.ProductionScale,
		CurrentTrackingMethod: req.CurrentTrackingMethod,
		FeaturesOfInterest:    req.FeaturesOfInterest,
		InterestedInTesting:   req.InterestedInTesting,
		Message:               req.Message,
		Source:                req.Source,
		Website:               req.Website,
	})
	if err != nil {
		var validationErr submissions.ValidationError
		if errors.As(err, &validationErr) {
			writeError(c, http.StatusBadRequest, "validation_error", "Please check the form and try again.")
			return
		}

		s.logger.Error("create early access submission failed", "error", err, "route", c.FullPath())
		writeError(c, http.StatusInternalServerError, "internal_error", "Something went wrong. Please try again later.")
		return
	}

	c.JSON(http.StatusOK, gin.H{
		"success": true,
		"message": "Thank you for your interest in MycoOrigyn.",
	})
}

func (s *server) resolveReview(c *gin.Context) {
	if s.approvals == nil {
		writeError(c, http.StatusServiceUnavailable, "service_unavailable", "Review is temporarily unavailable.")
		return
	}
	var req capabilityRequest
	if err := decodeRequestBody(c, &req); err != nil {
		s.writeDecodeError(c, err)
		return
	}
	application, err := s.approvals.Resolve(c.Request.Context(), req.Token)
	if err != nil {
		s.writeApprovalError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, application)
}

func (s *server) approveReview(c *gin.Context) {
	if s.approvals == nil {
		writeError(c, http.StatusServiceUnavailable, "service_unavailable", "Review is temporarily unavailable.")
		return
	}
	var req capabilityRequest
	if err := decodeRequestBody(c, &req); err != nil {
		s.writeDecodeError(c, err)
		return
	}
	result, err := s.approvals.Approve(c.Request.Context(), req.Token)
	if errors.Is(err, approvals.ErrDeliveryFailed) {
		writeError(c, http.StatusServiceUnavailable, "approval_delivery_failed", "Approval was saved, but email delivery failed. Retry the same approval.")
		return
	}
	if err != nil {
		s.writeApprovalError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, result)
}

func (s *server) declineReview(c *gin.Context) {
	if s.approvals == nil {
		writeError(c, http.StatusServiceUnavailable, "service_unavailable", "Review is temporarily unavailable.")
		return
	}
	var req capabilityRequest
	if err := decodeRequestBody(c, &req); err != nil {
		s.writeDecodeError(c, err)
		return
	}
	if err := s.approvals.Decline(c.Request.Context(), req.Token); err != nil {
		s.writeApprovalError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, gin.H{"status": "declined"})
}

func (s *server) validateSignupGrant(c *gin.Context) {
	s.handleGrantOperation(c, "validate")
}

func (s *server) claimSignupGrant(c *gin.Context) {
	s.handleGrantOperation(c, "claim")
}

func (s *server) consumeSignupGrant(c *gin.Context) {
	s.handleGrantOperation(c, "consume")
}

func (s *server) releaseSignupGrant(c *gin.Context) {
	s.handleGrantOperation(c, "release")
}

func (s *server) handleGrantOperation(c *gin.Context, operation string) {
	if s.approvals == nil {
		writeError(c, http.StatusServiceUnavailable, "service_unavailable", "Signup grants are temporarily unavailable.")
		return
	}
	var req signupGrantRequest
	if err := decodeRequestBody(c, &req); err != nil {
		s.writeDecodeError(c, err)
		return
	}
	var (
		grant approvals.Grant
		err   error
	)
	switch operation {
	case "validate":
		grant, err = s.approvals.ValidateGrant(c.Request.Context(), req.Token, req.Email)
	case "claim":
		grant, err = s.approvals.ClaimGrant(c.Request.Context(), req.Token, req.Email, req.ClaimReference)
	case "consume":
		grant, err = s.approvals.ConsumeGrant(c.Request.Context(), req.Token, req.Email, req.ClaimReference)
	case "release":
		grant, err = s.approvals.ReleaseGrant(c.Request.Context(), req.Token, req.Email, req.ClaimReference)
	}
	if err != nil {
		s.writeGrantError(c, err)
		return
	}
	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, grant)
}

func (s *server) writeApprovalError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, approvals.ErrInvalidReview):
		writeError(c, http.StatusNotFound, "review_not_found", "This review link is invalid or no longer available.")
	case errors.Is(err, approvals.ErrReviewExpired):
		writeError(c, http.StatusGone, "review_expired", "This review link has expired.")
	case errors.Is(err, approvals.ErrDecisionConflict):
		writeError(c, http.StatusConflict, "review_conflict", "This request has already received a different decision.")
	default:
		s.logger.Error("early access review operation failed", "route", c.FullPath())
		writeError(c, http.StatusInternalServerError, "internal_error", "Something went wrong. Please try again later.")
	}
}

func (s *server) writeGrantError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, approvals.ErrInvalidGrant), errors.Is(err, approvals.ErrInvalidInput):
		writeError(c, http.StatusNotFound, "signup_grant_not_found", "The signup grant is invalid or unavailable.")
	case errors.Is(err, approvals.ErrGrantExpired):
		writeError(c, http.StatusGone, "signup_grant_expired", "The signup grant has expired.")
	case errors.Is(err, approvals.ErrGrantClaimed):
		writeError(c, http.StatusConflict, "signup_grant_claimed", "The signup grant is already claimed by another operation.")
	case errors.Is(err, approvals.ErrInvalidClaim):
		writeError(c, http.StatusConflict, "signup_grant_claim_conflict", "The signup grant claim does not match.")
	default:
		s.logger.Error("signup grant operation failed", "route", c.FullPath())
		writeError(c, http.StatusInternalServerError, "internal_error", "Something went wrong. Please try again later.")
	}
}

func (s *server) recordVisitorCount(c *gin.Context) {
	req, err := decodeVisitorCountRequest(c)
	if err != nil {
		s.writeDecodeError(c, err)
		return
	}

	counts, err := s.pageViews.Record(c.Request.Context(), pageviews.RecordPageViewRequest{
		Page:      req.Page,
		VisitorID: req.VisitorID,
	})
	if err != nil {
		var validationErr pageviews.ValidationError
		if errors.As(err, &validationErr) {
			writeError(c, http.StatusBadRequest, "validation_error", "Please check the request and try again.")
			return
		}

		s.logger.Error("record visitor count failed", "error", err, "route", c.FullPath())
		writeError(c, http.StatusInternalServerError, "internal_error", "Something went wrong. Please try again later.")
		return
	}

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, counts)
}

func (s *server) getVisitorCount(c *gin.Context) {
	counts, err := s.pageViews.Get(c.Request.Context(), c.Query("page"))
	if err != nil {
		var validationErr pageviews.ValidationError
		if errors.As(err, &validationErr) {
			writeError(c, http.StatusBadRequest, "validation_error", "Please check the request and try again.")
			return
		}

		s.logger.Error("get visitor count failed", "error", err, "route", c.FullPath())
		writeError(c, http.StatusInternalServerError, "internal_error", "Something went wrong. Please try again later.")
		return
	}

	c.Header("Cache-Control", "no-store")
	c.JSON(http.StatusOK, counts)
}

func (s *server) earlyAccessOptions(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

func (s *server) publicOptions(c *gin.Context) {
	c.Status(http.StatusNoContent)
}

func (s *server) writeDecodeError(c *gin.Context, err error) {
	switch {
	case errors.Is(err, errMalformedJSON):
		writeError(c, http.StatusBadRequest, "malformed_json", "Please refresh the page and try again.")
	case errors.Is(err, errRequestTooLarge):
		writeError(c, http.StatusRequestEntityTooLarge, "request_too_large", "Please shorten your submission and try again.")
	default:
		writeError(c, http.StatusBadRequest, "validation_error", "Please check the form and try again.")
	}
}

func decodeEarlyAccessRequest(c *gin.Context) (earlyAccessRequest, error) {
	var req earlyAccessRequest

	if err := decodeRequestBody(c, &req); err != nil {
		return earlyAccessRequest{}, err
	}

	return req, nil
}

func decodeVisitorCountRequest(c *gin.Context) (visitorCountRequest, error) {
	var req visitorCountRequest

	if err := decodeRequestBody(c, &req); err != nil {
		return visitorCountRequest{}, err
	}

	return req, nil
}

func decodeRequestBody(c *gin.Context, target interface{}) error {
	decoder := json.NewDecoder(c.Request.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return errRequestTooLarge
		}
		return errMalformedJSON
	}

	var extra struct{}
	if err := decoder.Decode(&extra); !errors.Is(err, io.EOF) {
		var maxBytesErr *http.MaxBytesError
		if errors.As(err, &maxBytesErr) {
			return errRequestTooLarge
		}
		return errMalformedJSON
	}

	return nil
}

var (
	errMalformedJSON   = errors.New("malformed json")
	errRequestTooLarge = errors.New("request too large")
)

func requestLogger(logger *slog.Logger) gin.HandlerFunc {
	return func(c *gin.Context) {
		start := time.Now()
		c.Next()
		logger.Info(
			"http request",
			"method", c.Request.Method,
			"path", c.FullPath(),
			"status", c.Writer.Status(),
			"duration_ms", time.Since(start).Milliseconds(),
			"client_ip", c.ClientIP(),
		)
	}
}

func recoveryHandler(logger *slog.Logger) gin.RecoveryFunc {
	return func(c *gin.Context, recovered any) {
		logger.Error("panic recovered", "panic", recovered, "path", c.FullPath())
		writeError(c, http.StatusInternalServerError, "internal_error", "Something went wrong. Please try again later.")
	}
}

func publicCORS(allowedOrigins map[string]struct{}) gin.HandlerFunc {
	return func(c *gin.Context) {
		origin := strings.TrimSpace(c.GetHeader("Origin"))
		if origin != "" {
			if _, ok := allowedOrigins[origin]; ok {
				headers := c.Writer.Header()
				headers.Set("Access-Control-Allow-Origin", origin)
				headers.Set("Access-Control-Allow-Methods", "GET, POST, OPTIONS")
				headers.Set("Access-Control-Allow-Headers", "Content-Type")
				headers.Set("Access-Control-Max-Age", "600")
				headers.Add("Vary", "Origin")
				headers.Add("Vary", "Access-Control-Request-Method")
				headers.Add("Vary", "Access-Control-Request-Headers")
			}
		}
		c.Next()
	}
}

func limitRequestBody(maxBytes int64) gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.ContentLength > maxBytes {
			writeError(c, http.StatusRequestEntityTooLarge, "request_too_large", "Please shorten your submission and try again.")
			c.Abort()
			return
		}

		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBytes)
		}
		c.Next()
	}
}

func writeError(c *gin.Context, status int, code, message string) {
	c.AbortWithStatusJSON(status, gin.H{
		"error": gin.H{
			"code":    code,
			"message": message,
		},
	})
}

func requireServiceAuthentication(expected []byte) gin.HandlerFunc {
	expectedDigest := sha256.Sum256(expected)
	configured := len(expected) >= 32
	return func(c *gin.Context) {
		if !configured {
			writeError(c, http.StatusServiceUnavailable, "service_unavailable", "Provisioning authorization is unavailable.")
			return
		}
		header := c.GetHeader("Authorization")
		provided, ok := strings.CutPrefix(header, "Bearer ")
		providedDigest := sha256.Sum256([]byte(provided))
		if !ok || provided == "" || subtle.ConstantTimeCompare(expectedDigest[:], providedDigest[:]) != 1 {
			writeError(c, http.StatusUnauthorized, "service_authentication_required", "Service authentication is required.")
			return
		}
		c.Next()
	}
}

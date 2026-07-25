package httpapi

import (
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/pageviews"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/submissions"
)

type Options struct {
	CORSAllowedOrigins []string
	Logger             *slog.Logger
}

func NewServer(service submissions.Service, pageViews pageviews.CounterService, opts Options) *gin.Engine {
	logger := opts.Logger
	if logger == nil {
		logger = slog.New(slog.NewJSONHandler(os.Stdout, nil))
	}

	server := &server{
		service:            service,
		pageViews:          pageViews,
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
	public.POST("/visitor-count", server.recordVisitorCount)
	public.GET("/visitor-count", server.getVisitorCount)
	public.OPTIONS("/visitor-count", server.publicOptions)

	return router
}

type server struct {
	service            submissions.Service
	pageViews          pageviews.CounterService
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

package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/pageviews"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/securetokens"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/submissions"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/transactionalemail"
)

type storedSubmission struct {
	submission submissions.Submission
	createdAt  time.Time
}

type fakePageViews struct {
	mu       sync.Mutex
	counts   map[string]pageviews.VisitorCounter
	visitors map[string]map[string]struct{}
}

type fakeRepository struct {
	mu          sync.Mutex
	now         time.Time
	submissions []storedSubmission
}

func newFakeRepository() *fakeRepository {
	return &fakeRepository{now: time.Date(2026, 7, 10, 12, 0, 0, 0, time.UTC)}
}

func (f *fakeRepository) HasRecentFingerprint(_ context.Context, fingerprint string, since time.Time) (bool, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	for _, item := range f.submissions {
		if item.submission.PayloadFingerprint == fingerprint && !item.createdAt.Before(since) {
			return true, nil
		}
	}
	return false, nil
}

func (f *fakeRepository) CreateSubmission(_ context.Context, submission submissions.Submission, since time.Time, _ *submissions.ReviewCapability) (submissions.CreateResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	for i, item := range f.submissions {
		if item.submission.PayloadFingerprint == submission.PayloadFingerprint && !item.createdAt.Before(since) {
			return submissions.CreateResult{SubmissionID: fmt.Sprintf("submission-%d", i+1), Created: false}, nil
		}
	}
	f.submissions = append(f.submissions, storedSubmission{submission: submission, createdAt: f.now})
	return submissions.CreateResult{SubmissionID: fmt.Sprintf("submission-%d", len(f.submissions)), Created: true}, nil
}

func (f *fakeRepository) CreateEarlyAccessSubmission(_ context.Context, submission submissions.Submission) error {
	f.mu.Lock()
	defer f.mu.Unlock()

	f.submissions = append(f.submissions, storedSubmission{
		submission: submission,
		createdAt:  f.now,
	})
	return nil
}

func (f *fakeRepository) count() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.submissions)
}

func (f *fakeRepository) last() submissions.Submission {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.submissions[len(f.submissions)-1].submission
}

func (f *fakePageViews) Record(_ context.Context, req pageviews.RecordPageViewRequest) (pageviews.VisitorCounter, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	page := req.Page
	if page == "" {
		page = "landing"
	}

	if f.counts == nil {
		f.counts = make(map[string]pageviews.VisitorCounter)
	}
	if f.visitors == nil {
		f.visitors = make(map[string]map[string]struct{})
	}

	count := f.counts[page]
	count.Page = page
	count.TotalVisits++

	visitorID := req.VisitorID
	if visitorID != "" {
		visitorIDs, ok := f.visitors[page]
		if !ok {
			visitorIDs = make(map[string]struct{})
			f.visitors[page] = visitorIDs
		}

		if _, seen := visitorIDs[visitorID]; !seen {
			count.UniqueVisitors++
			visitorIDs[visitorID] = struct{}{}
		}
	}

	f.counts[page] = count
	return count, nil
}

func (f *fakePageViews) Get(_ context.Context, pagePath string) (pageviews.VisitorCounter, error) {
	f.mu.Lock()
	defer f.mu.Unlock()

	if pagePath == "" {
		pagePath = "landing"
	}
	count, ok := f.counts[pagePath]
	if !ok {
		return pageviews.VisitorCounter{Page: pagePath}, nil
	}
	count.Page = pagePath
	return count, nil
}

func newFakePageViews() *fakePageViews {
	return &fakePageViews{
		counts:   make(map[string]pageviews.VisitorCounter),
		visitors: make(map[string]map[string]struct{}),
	}
}

func testServer(t *testing.T, origins []string) (*gin.Engine, *fakeRepository, *fakePageViews) {
	t.Helper()

	gin.SetMode(gin.TestMode)
	repo := newFakeRepository()
	pageViews := newFakePageViews()
	service := submissions.NewService(repo, submissions.ServiceOptions{
		Now:           func() time.Time { return repo.now },
		Tokens:        securetokens.NewMemoryStore(),
		Email:         &transactionalemail.MemorySender{},
		EmailFrom:     "MycoOrigyn <notify@example.com>",
		ReviewerEmail: "reviewer@example.com",
		ReviewBaseURL: "https://mycoorigyn.com/early-access/review",
	})

	return NewServer(service, pageViews, Options{CORSAllowedOrigins: origins}), repo, pageViews
}

func TestValidWaitlistSubmission(t *testing.T) {
	handler, repo, _ := testServer(t, []string{"https://mycoorigyn.com"})

	resp := postJSON(t, handler, map[string]any{
		"submission_type": "waitlist",
		"email":           "  PERSON@Example.COM ",
		"name":            " Person ",
		"website":         "",
	})

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}
	assertSuccess(t, resp.Body.Bytes())

	got := repo.last()
	if got.Email != "person@example.com" {
		t.Fatalf("email = %q, want normalized email", got.Email)
	}
	if got.Name != "Person" {
		t.Fatalf("name = %q, want trimmed value", got.Name)
	}
	if got.Source != submissions.DefaultSource {
		t.Fatalf("source = %q, want %q", got.Source, submissions.DefaultSource)
	}
	if got.Status != submissions.DefaultStatus {
		t.Fatalf("status = %q, want %q", got.Status, submissions.DefaultStatus)
	}
}

func TestValidEarlyAccessSubmission(t *testing.T) {
	handler, repo, _ := testServer(t, nil)
	testingInterest := true

	resp := postJSON(t, handler, map[string]any{
		"submission_type":         "early_access",
		"email":                   "grower@example.com",
		"name":                    " Jane Grower ",
		"farm_name":               " Example Mushroom Farm ",
		"farm_type":               " Boutique gourmet farm ",
		"production_scale":        " 50-100 lb/week ",
		"current_tracking_method": " Spreadsheets and paper logs ",
		"features_of_interest":    []string{" Mobile access ", "Traceability"},
		"interested_in_testing":   testingInterest,
		"message":                 " Interested in testing the prototype. ",
		"source":                  " marketing_site ",
		"website":                 "",
	})

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}

	got := repo.last()
	if got.SubmissionType != submissions.TypeEarlyAccess {
		t.Fatalf("submission_type = %q", got.SubmissionType)
	}
	if got.Name != "Jane Grower" {
		t.Fatalf("name = %q", got.Name)
	}
	if got.FarmName != "Example Mushroom Farm" {
		t.Fatalf("farm_name = %q", got.FarmName)
	}
	if len(got.FeaturesOfInterest) != 2 || got.FeaturesOfInterest[0] != "Mobile access" || got.FeaturesOfInterest[1] != "Traceability" {
		t.Fatalf("features = %#v", got.FeaturesOfInterest)
	}
	if got.InterestedInTesting == nil || !*got.InterestedInTesting {
		t.Fatalf("interested_in_testing not stored")
	}
}

func TestMissingEmail(t *testing.T) {
	handler, _, _ := testServer(t, nil)

	resp := postJSON(t, handler, map[string]any{
		"submission_type": "waitlist",
		"website":         "",
	})

	assertErrorCode(t, resp, http.StatusBadRequest, "validation_error")
}

func TestInvalidEmail(t *testing.T) {
	handler, _, _ := testServer(t, nil)

	resp := postJSON(t, handler, map[string]any{
		"submission_type": "waitlist",
		"email":           "not an email",
		"website":         "",
	})

	assertErrorCode(t, resp, http.StatusBadRequest, "validation_error")
}

func TestUnsupportedSubmissionType(t *testing.T) {
	handler, _, _ := testServer(t, nil)

	resp := postJSON(t, handler, map[string]any{
		"submission_type": "demo",
		"email":           "person@example.com",
		"website":         "",
	})

	assertErrorCode(t, resp, http.StatusBadRequest, "validation_error")
}

func TestOversizedFieldValue(t *testing.T) {
	handler, _, _ := testServer(t, nil)

	resp := postJSON(t, handler, map[string]any{
		"submission_type": "waitlist",
		"email":           "person@example.com",
		"farm_type":       strings.Repeat("a", submissions.MaxFarmTypeLength+1),
		"website":         "",
	})

	assertErrorCode(t, resp, http.StatusBadRequest, "validation_error")
}

func TestTooManyFeaturesOfInterest(t *testing.T) {
	handler, _, _ := testServer(t, nil)

	features := make([]string, submissions.MaxFeatureCount+1)
	for i := range features {
		features[i] = "Feature"
	}

	resp := postJSON(t, handler, map[string]any{
		"submission_type":      "early_access",
		"email":                "person@example.com",
		"features_of_interest": features,
		"website":              "",
	})

	assertErrorCode(t, resp, http.StatusBadRequest, "validation_error")
}

func TestOversizedFeatureLabel(t *testing.T) {
	handler, _, _ := testServer(t, nil)

	resp := postJSON(t, handler, map[string]any{
		"submission_type":      "early_access",
		"email":                "person@example.com",
		"features_of_interest": []string{strings.Repeat("a", submissions.MaxFeatureLength+1)},
		"website":              "",
	})

	assertErrorCode(t, resp, http.StatusBadRequest, "validation_error")
}

func TestNonEmptyHoneypot(t *testing.T) {
	handler, repo, _ := testServer(t, nil)

	resp := postJSON(t, handler, map[string]any{
		"submission_type": "waitlist",
		"email":           "person@example.com",
		"website":         "https://spam.example",
	})

	assertErrorCode(t, resp, http.StatusBadRequest, "validation_error")
	if repo.count() != 0 {
		t.Fatalf("store count = %d, want 0", repo.count())
	}
}

func TestMalformedJSON(t *testing.T) {
	handler, _, _ := testServer(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/public/early-access", strings.NewReader(`{"email":`))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	assertErrorCode(t, resp, http.StatusBadRequest, "malformed_json")
}

func TestUnknownJSONField(t *testing.T) {
	handler, _, _ := testServer(t, nil)

	resp := postJSON(t, handler, map[string]any{
		"submission_type": "waitlist",
		"email":           "person@example.com",
		"unexpected":      "nope",
		"website":         "",
	})

	assertErrorCode(t, resp, http.StatusBadRequest, "malformed_json")
}

func TestOversizedRequestBody(t *testing.T) {
	handler, _, _ := testServer(t, nil)

	req := httptest.NewRequest(http.MethodPost, "/public/early-access", strings.NewReader(strings.Repeat("a", int(submissions.MaxRequestBodyBytes)+1)))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	assertErrorCode(t, resp, http.StatusRequestEntityTooLarge, "request_too_large")
}

func TestSafeDuplicateBehavior(t *testing.T) {
	handler, repo, _ := testServer(t, nil)
	payload := map[string]any{
		"submission_type": "waitlist",
		"email":           "person@example.com",
		"website":         "",
	}

	first := postJSON(t, handler, payload)
	second := postJSON(t, handler, payload)

	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("statuses = %d, %d", first.Code, second.Code)
	}
	if repo.count() != 1 {
		t.Fatalf("count = %d, want 1", repo.count())
	}
}

func TestDuplicateEmailWithDifferentPayloadAllowed(t *testing.T) {
	handler, repo, _ := testServer(t, nil)

	first := postJSON(t, handler, map[string]any{
		"submission_type": "waitlist",
		"email":           "person@example.com",
		"source":          "marketing_site",
		"website":         "",
	})
	second := postJSON(t, handler, map[string]any{
		"submission_type": "waitlist",
		"email":           "person@example.com",
		"source":          "expo_landing_page",
		"website":         "",
	})

	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("statuses = %d, %d", first.Code, second.Code)
	}
	if repo.count() != 2 {
		t.Fatalf("count = %d, want 2", repo.count())
	}
}

func TestWaitlistThenEarlyAccessAllowed(t *testing.T) {
	handler, repo, _ := testServer(t, nil)

	first := postJSON(t, handler, map[string]any{
		"submission_type": "waitlist",
		"email":           "grower@example.com",
		"website":         "",
	})
	second := postJSON(t, handler, map[string]any{
		"submission_type":      "early_access",
		"email":                "grower@example.com",
		"features_of_interest": []string{"Traceability"},
		"website":              "",
	})

	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("statuses = %d, %d", first.Code, second.Code)
	}
	if repo.count() != 2 {
		t.Fatalf("count = %d, want 2", repo.count())
	}
}

func TestVisitorCountIncrementsAndDedupesByVisitor(t *testing.T) {
	handler, _, _ := testServer(t, nil)

	first := postJSONToEndpoint(t, handler, "/public/visitor-count", map[string]any{
		"page":       "landing",
		"visitor_id": "visitor-1",
	})
	second := postJSONToEndpoint(t, handler, "/public/visitor-count", map[string]any{
		"page":       "landing",
		"visitor_id": "visitor-1",
	})
	third := postJSONToEndpoint(t, handler, "/public/visitor-count", map[string]any{
		"page":       "landing",
		"visitor_id": "visitor-2",
	})

	if first.Code != http.StatusOK || second.Code != http.StatusOK || third.Code != http.StatusOK {
		t.Fatalf("statuses = %d, %d, %d", first.Code, second.Code, third.Code)
	}

	if got := parseVisitorCounter(t, first.Body.Bytes()); got.TotalVisits != 1 || got.UniqueVisitors != 1 {
		t.Fatalf("first response = %#v", got)
	}

	if got := parseVisitorCounter(t, second.Body.Bytes()); got.TotalVisits != 2 || got.UniqueVisitors != 1 {
		t.Fatalf("second response = %#v", got)
	}

	if got := parseVisitorCounter(t, third.Body.Bytes()); got.TotalVisits != 3 || got.UniqueVisitors != 2 {
		t.Fatalf("third response = %#v", got)
	}

}

func TestGetVisitorCountFromStoredCounts(t *testing.T) {
	handler, _, _ := testServer(t, nil)

	postJSONToEndpoint(t, handler, "/public/visitor-count", map[string]any{
		"page":       "landing",
		"visitor_id": "visitor-1",
	})

	resp := getJSON(t, handler, "/public/visitor-count?page=landing")
	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d; body=%s", resp.Code, http.StatusOK, resp.Body.String())
	}

	got := parseVisitorCounter(t, resp.Body.Bytes())
	if got.TotalVisits != 1 || got.UniqueVisitors != 1 {
		t.Fatalf("visitor-count = %#v", got)
	}
}

func TestPublicRouteRequiresNoAuth(t *testing.T) {
	handler, _, _ := testServer(t, nil)

	resp := postJSON(t, handler, map[string]any{
		"submission_type": "waitlist",
		"email":           "person@example.com",
		"website":         "",
	})

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}
}

func TestCORSPreflightAllowedOrigin(t *testing.T) {
	handler, _, _ := testServer(t, []string{"https://mycoorigyn.com"})

	req := httptest.NewRequest(http.MethodOptions, "/public/early-access", nil)
	req.Header.Set("Origin", "https://mycoorigyn.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusNoContent)
	}
	if got := resp.Header().Get("Access-Control-Allow-Origin"); got != "https://mycoorigyn.com" {
		t.Fatalf("allow origin = %q", got)
	}
	if got := resp.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodPost) {
		t.Fatalf("allow methods = %q", got)
	}
}

func TestCORSPreflightDisallowedOrigin(t *testing.T) {
	handler, _, _ := testServer(t, []string{"https://mycoorigyn.com"})

	req := httptest.NewRequest(http.MethodOptions, "/public/early-access", nil)
	req.Header.Set("Origin", "https://example.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodPost)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusNoContent)
	}
	if got := resp.Header().Get("Access-Control-Allow-Origin"); got != "" {
		t.Fatalf("allow origin = %q, want empty", got)
	}
}

func TestCORSPreflightVisitorCountAllowsGetAndPost(t *testing.T) {
	handler, _, _ := testServer(t, []string{"https://mycoorigyn.com"})

	req := httptest.NewRequest(http.MethodOptions, "/public/visitor-count", nil)
	req.Header.Set("Origin", "https://mycoorigyn.com")
	req.Header.Set("Access-Control-Request-Method", http.MethodGet)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusNoContent {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusNoContent)
	}
	if got := resp.Header().Get("Access-Control-Allow-Methods"); !strings.Contains(got, http.MethodGet) {
		t.Fatalf("allow methods = %q", got)
	}
}

func TestHealthzReturnsOK(t *testing.T) {
	handler, _, _ := testServer(t, nil)

	req := httptest.NewRequest(http.MethodGet, "/healthz", nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)

	if resp.Code != http.StatusOK {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusOK)
	}

	var payload map[string]string
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if payload["status"] != "ok" {
		t.Fatalf("status payload = %#v", payload)
	}
}

func TestErrorResponseShapeIsStructured(t *testing.T) {
	handler, _, _ := testServer(t, nil)

	resp := postJSON(t, handler, map[string]any{
		"submission_type": "waitlist",
		"website":         "",
	})

	if resp.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", resp.Code, http.StatusBadRequest)
	}

	var payload struct {
		Error struct {
			Code    string `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload.Error.Code == "" || payload.Error.Message == "" {
		t.Fatalf("error payload = %#v", payload)
	}
}

func postJSON(t *testing.T, handler http.Handler, payload any) *httptest.ResponseRecorder {
	return postJSONToEndpoint(t, handler, "/public/early-access", payload)
}

func postJSONToEndpoint(t *testing.T, handler http.Handler, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()

	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}

	req := httptest.NewRequest(http.MethodPost, path, bytes.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func getJSON(t *testing.T, handler http.Handler, path string) *httptest.ResponseRecorder {
	t.Helper()

	req := httptest.NewRequest(http.MethodGet, path, nil)
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

func parseVisitorCounter(t *testing.T, body []byte) pageviews.VisitorCounter {
	t.Helper()

	var payload pageviews.VisitorCounter
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode visitor counter response: %v", err)
	}
	return payload
}

func assertSuccess(t *testing.T, body []byte) {
	t.Helper()

	var payload struct {
		Success bool   `json:"success"`
		Message string `json:"message"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		t.Fatalf("decode success response: %v", err)
	}
	if !payload.Success {
		t.Fatalf("success = false")
	}
	if payload.Message != "Thank you for your interest in MycoOrigyn." {
		t.Fatalf("message = %q", payload.Message)
	}
}

func assertErrorCode(t *testing.T, resp *httptest.ResponseRecorder, status int, code string) {
	t.Helper()

	if resp.Code != status {
		t.Fatalf("status = %d, want %d; body=%s", resp.Code, status, resp.Body.String())
	}

	var payload struct {
		Error struct {
			Code string `json:"code"`
		} `json:"error"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &payload); err != nil {
		t.Fatalf("decode error response: %v", err)
	}
	if payload.Error.Code != code {
		t.Fatalf("error code = %q, want %q; body=%s", payload.Error.Code, code, resp.Body.String())
	}
}

package httpapi

import (
	"context"
	"crypto/subtle"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/approvals"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/securetokens"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/submissions"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/transactionalemail"
)

type reviewRepository struct {
	mu           sync.Mutex
	reviewDigest []byte
	application  approvals.Application
	approval     *approvals.Approval
	approveCount int
	declineCount int
}

type consumeReplayRepository struct {
	mu          sync.Mutex
	tokenDigest []byte
	claimDigest []byte
	grant       approvals.Grant
}

func (*consumeReplayRepository) ResolveReview(context.Context, []byte, time.Time) (approvals.Application, error) {
	return approvals.Application{}, approvals.ErrInvalidReview
}

func (*consumeReplayRepository) Approve(context.Context, []byte, approvals.GrantCandidate, time.Time) (approvals.Approval, bool, error) {
	return approvals.Approval{}, false, approvals.ErrInvalidReview
}

func (*consumeReplayRepository) Decline(context.Context, []byte, time.Time) error {
	return approvals.ErrInvalidReview
}

func (r *consumeReplayRepository) ValidateGrant(_ context.Context, digest []byte, email string, now time.Time) (approvals.Grant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.matches(digest, email) || !now.Before(r.grant.ExpiresAt) || (r.grant.Status != "active" && r.grant.Status != "claimed") {
		return approvals.Grant{}, approvals.ErrInvalidGrant
	}
	return r.grant, nil
}

func (r *consumeReplayRepository) ClaimGrant(_ context.Context, digest []byte, email string, claimDigest []byte, now, claimExpiresAt time.Time) (approvals.Grant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.matches(digest, email) || !now.Before(r.grant.ExpiresAt) {
		return approvals.Grant{}, approvals.ErrInvalidGrant
	}
	switch r.grant.Status {
	case "active":
		r.claimDigest = append([]byte(nil), claimDigest...)
		r.grant.Status = "claimed"
		r.grant.ClaimExpiresAt = claimExpiresAt
	case "claimed":
		if subtle.ConstantTimeCompare(r.claimDigest, claimDigest) != 1 || !now.Before(r.grant.ClaimExpiresAt) {
			return approvals.Grant{}, approvals.ErrGrantClaimed
		}
	default:
		return approvals.Grant{}, approvals.ErrInvalidGrant
	}
	return r.grant, nil
}

func (r *consumeReplayRepository) ConsumeGrant(_ context.Context, digest []byte, email string, claimDigest []byte, now time.Time) (approvals.Grant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.matches(digest, email) {
		return approvals.Grant{}, approvals.ErrInvalidGrant
	}
	if r.grant.Status == "consumed" {
		if subtle.ConstantTimeCompare(r.claimDigest, claimDigest) != 1 {
			return approvals.Grant{}, approvals.ErrInvalidClaim
		}
		return r.grant, nil
	}
	if r.grant.Status != "claimed" || !now.Before(r.grant.ExpiresAt) || !now.Before(r.grant.ClaimExpiresAt) || subtle.ConstantTimeCompare(r.claimDigest, claimDigest) != 1 {
		return approvals.Grant{}, approvals.ErrInvalidClaim
	}
	r.grant.Status = "consumed"
	r.grant.ClaimExpiresAt = time.Time{}
	return r.grant, nil
}

func (r *consumeReplayRepository) ReleaseGrant(_ context.Context, digest []byte, email string, claimDigest []byte, now time.Time) (approvals.Grant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if !r.matches(digest, email) || r.grant.Status != "claimed" || !now.Before(r.grant.ClaimExpiresAt) || subtle.ConstantTimeCompare(r.claimDigest, claimDigest) != 1 {
		return approvals.Grant{}, approvals.ErrInvalidClaim
	}
	r.grant.Status = "active"
	r.grant.ClaimExpiresAt = time.Time{}
	r.claimDigest = nil
	return r.grant, nil
}

func (r *consumeReplayRepository) matches(digest []byte, email string) bool {
	return subtle.ConstantTimeCompare(r.tokenDigest, digest) == 1 && email == r.grant.ApprovedEmail
}

func (r *reviewRepository) ResolveReview(_ context.Context, digest []byte, now time.Time) (approvals.Application, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if subtle.ConstantTimeCompare(digest, r.reviewDigest) != 1 {
		return approvals.Application{}, approvals.ErrInvalidReview
	}
	if !now.Before(r.application.ReviewExpiresAt) {
		return approvals.Application{}, approvals.ErrReviewExpired
	}
	if r.application.ApprovalStatus != "pending" {
		return approvals.Application{}, approvals.ErrInvalidReview
	}
	return r.application, nil
}

func (r *reviewRepository) Approve(_ context.Context, digest []byte, candidate approvals.GrantCandidate, _ time.Time) (approvals.Approval, bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if subtle.ConstantTimeCompare(digest, r.reviewDigest) != 1 {
		return approvals.Approval{}, false, approvals.ErrInvalidReview
	}
	r.approveCount++
	if r.approval != nil {
		return *r.approval, false, nil
	}
	r.application.ApprovalStatus = "approved"
	r.approval = &approvals.Approval{
		ApplicationID: r.application.ID, GrantID: "grant-1", ApprovedEmail: r.application.Email,
		FarmName: r.application.FarmName, TokenDigest: candidate.TokenDigest,
		SecretReference: candidate.SecretReference, ExpiresAt: candidate.ExpiresAt,
	}
	return *r.approval, true, nil
}

func (r *reviewRepository) Decline(_ context.Context, digest []byte, _ time.Time) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if subtle.ConstantTimeCompare(digest, r.reviewDigest) != 1 {
		return approvals.ErrInvalidReview
	}
	r.declineCount++
	r.application.ApprovalStatus = "declined"
	return nil
}

func (r *reviewRepository) ValidateGrant(_ context.Context, digest []byte, email string, _ time.Time) (approvals.Grant, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.approval == nil || subtle.ConstantTimeCompare(digest, r.approval.TokenDigest) != 1 || email != r.approval.ApprovedEmail {
		return approvals.Grant{}, approvals.ErrInvalidGrant
	}
	return approvals.Grant{ID: r.approval.GrantID, ApprovedEmail: email, Source: approvals.GrantSourceEarlyAccess, Status: "active", ExpiresAt: r.approval.ExpiresAt}, nil
}

func (r *reviewRepository) ClaimGrant(ctx context.Context, digest []byte, email string, _ []byte, now, claimExpiresAt time.Time) (approvals.Grant, error) {
	grant, err := r.ValidateGrant(ctx, digest, email, now)
	grant.Status = "claimed"
	grant.ClaimExpiresAt = claimExpiresAt
	return grant, err
}

func (r *reviewRepository) ConsumeGrant(ctx context.Context, digest []byte, email string, _ []byte, now time.Time) (approvals.Grant, error) {
	grant, err := r.ValidateGrant(ctx, digest, email, now)
	grant.Status = "consumed"
	return grant, err
}

func (r *reviewRepository) ReleaseGrant(ctx context.Context, digest []byte, email string, _ []byte, now time.Time) (approvals.Grant, error) {
	return r.ValidateGrant(ctx, digest, email, now)
}

func newReviewServer(t *testing.T) (*gin.Engine, *reviewRepository, string, *transactionalemail.MemorySender, []byte) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	reviewToken, reviewDigest, err := securetokens.Generate()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	repo := &reviewRepository{
		reviewDigest: reviewDigest,
		application: approvals.Application{
			ID: "submission-1", Name: "Owner", Email: "owner@example.com", FarmName: "Example Farm",
			ApprovalStatus: "pending", ReviewExpiresAt: now.Add(time.Hour),
		},
	}
	tokens := securetokens.NewMemoryStore()
	sender := &transactionalemail.MemorySender{}
	service := approvals.NewService(repo, approvals.ServiceOptions{
		Tokens: tokens, Email: sender, From: "notify@example.com",
		SignupBaseURL: "https://app.example.com/signup", Now: func() time.Time { return now },
	})
	secret := []byte("0123456789abcdef0123456789abcdef")
	emptySubmissions := submissions.NewService(&submissionRepositoryForReview{}, submissions.ServiceOptions{})
	pageViewService := newFakePageViews()
	return NewServer(emptySubmissions, pageViewService, Options{Approvals: &service, ProvisioningSecret: secret}), repo, reviewToken, sender, secret
}

type submissionRepositoryForReview struct{}

func (*submissionRepositoryForReview) CreateSubmission(context.Context, submissions.Submission, time.Time, *submissions.ReviewCapability) (submissions.CreateResult, error) {
	return submissions.CreateResult{Created: true, SubmissionID: "unused"}, nil
}

func TestReviewResolveIsPostOnlyAndDoesNotMutate(t *testing.T) {
	handler, repo, token, _, _ := newReviewServer(t)
	get := httptest.NewRequest(http.MethodGet, "/public/early-access/review/resolve", nil)
	getResp := httptest.NewRecorder()
	handler.ServeHTTP(getResp, get)
	if getResp.Code != http.StatusMethodNotAllowed || repo.approveCount != 0 || repo.declineCount != 0 {
		t.Fatalf("GET mutated or was not rejected: status=%d", getResp.Code)
	}

	resp := postJSONToEndpoint(t, handler, "/public/early-access/review/resolve", map[string]string{"token": token})
	if resp.Code != http.StatusOK || strings.Contains(resp.Body.String(), "token") || strings.Contains(resp.Body.String(), "submission-1") {
		t.Fatalf("unsafe resolve response: status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestExplicitApprovalSendsEmailWithoutReturningToken(t *testing.T) {
	handler, repo, token, sender, _ := newReviewServer(t)
	resp := postJSONToEndpoint(t, handler, "/public/early-access/review/approve", map[string]string{"token": token})
	if resp.Code != http.StatusOK || repo.approveCount != 1 || len(sender.Snapshot()) != 1 {
		t.Fatalf("approval failed: status=%d body=%s", resp.Code, resp.Body.String())
	}
	if strings.Contains(resp.Body.String(), "access=") || strings.Contains(resp.Body.String(), "token") {
		t.Fatalf("approval response exposed token: %s", resp.Body.String())
	}
}

func TestSignupGrantServiceAuthenticationRequired(t *testing.T) {
	handler, _, reviewToken, _, secret := newReviewServer(t)
	approved := postJSONToEndpoint(t, handler, "/public/early-access/review/approve", map[string]string{"token": reviewToken})
	if approved.Code != http.StatusOK {
		t.Fatalf("approve status=%d", approved.Code)
	}

	unauthorized := postJSONToEndpoint(t, handler, "/internal/signup-grants/validate", map[string]string{"token": "invalid", "email": "owner@example.com"})
	assertErrorCode(t, unauthorized, http.StatusUnauthorized, "service_authentication_required")

	body, _ := json.Marshal(map[string]string{"token": "invalid", "email": "owner@example.com"})
	req := httptest.NewRequest(http.MethodPost, "/internal/signup-grants/validate", strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(secret))
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	if resp.Code != http.StatusNotFound {
		t.Fatalf("authenticated invalid grant status=%d body=%s", resp.Code, resp.Body.String())
	}
}

func TestConsumeSignupGrantReplayReturnsSameSafeSuccess(t *testing.T) {
	gin.SetMode(gin.TestMode)
	token, tokenDigest, err := securetokens.Generate()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC)
	repo := &consumeReplayRepository{
		tokenDigest: tokenDigest,
		grant: approvals.Grant{
			ID: "grant-replay-1", ApprovedEmail: "owner@example.com", Source: approvals.GrantSourceEarlyAccess,
			Status: "active", ExpiresAt: now.Add(time.Hour),
		},
	}
	service := approvals.NewService(repo, approvals.ServiceOptions{Now: func() time.Time { return now }})
	secret := []byte("0123456789abcdef0123456789abcdef")
	handler := NewServer(
		submissions.NewService(&submissionRepositoryForReview{}, submissions.ServiceOptions{}),
		newFakePageViews(),
		Options{Approvals: &service, ProvisioningSecret: secret},
	)
	claimA := "stable-provision-operation-a"
	payload := map[string]string{"token": token, "email": "owner@example.com", "claim_reference": claimA}
	if resp := postAuthenticatedJSON(t, handler, secret, "/internal/signup-grants/claim", payload); resp.Code != http.StatusOK {
		t.Fatalf("claim status=%d body=%s", resp.Code, resp.Body.String())
	}
	first := postAuthenticatedJSON(t, handler, secret, "/internal/signup-grants/consume", payload)
	second := postAuthenticatedJSON(t, handler, secret, "/internal/signup-grants/consume", payload)
	if first.Code != http.StatusOK || second.Code != http.StatusOK {
		t.Fatalf("consume statuses=%d,%d bodies=%s / %s", first.Code, second.Code, first.Body.String(), second.Body.String())
	}
	if first.Body.String() != second.Body.String() {
		t.Fatalf("replay response changed: first=%s second=%s", first.Body.String(), second.Body.String())
	}
	var response map[string]any
	if err := json.Unmarshal(second.Body.Bytes(), &response); err != nil {
		t.Fatal(err)
	}
	if response["grant_id"] != "grant-replay-1" || response["status"] != "consumed" {
		t.Fatalf("unexpected replay response: %v", response)
	}
	for _, forbidden := range []string{token, claimA, "claim_reference", "digest", "secret"} {
		if strings.Contains(second.Body.String(), forbidden) {
			t.Fatalf("consume response exposed protected value or field %q: %s", forbidden, second.Body.String())
		}
	}
	wrongClaim := map[string]string{"token": token, "email": "owner@example.com", "claim_reference": "stable-provision-operation-b"}
	denied := postAuthenticatedJSON(t, handler, secret, "/internal/signup-grants/consume", wrongClaim)
	assertErrorCode(t, denied, http.StatusConflict, "signup_grant_claim_conflict")
}

func postAuthenticatedJSON(t *testing.T, handler http.Handler, secret []byte, path string, payload any) *httptest.ResponseRecorder {
	t.Helper()
	body, err := json.Marshal(payload)
	if err != nil {
		t.Fatal(err)
	}
	req := httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(body)))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+string(secret))
	resp := httptest.NewRecorder()
	handler.ServeHTTP(resp, req)
	return resp
}

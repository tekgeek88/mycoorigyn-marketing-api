package submissions

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/securetokens"
	"github.com/mycoorigyn/mycoorigyn-marketing-api/internal/transactionalemail"
)

type submissionRepository struct {
	mu      sync.Mutex
	created []Submission
}

func (r *submissionRepository) CreateSubmission(_ context.Context, submission Submission, _ time.Time, _ *ReviewCapability) (CreateResult, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	for _, existing := range r.created {
		if existing.PayloadFingerprint == submission.PayloadFingerprint {
			return CreateResult{SubmissionID: "submission-1", Created: false}, nil
		}
	}
	r.created = append(r.created, submission)
	return CreateResult{SubmissionID: "submission-1", Created: true}, nil
}

func TestEarlyAccessNotifiesReviewerOnceAndUsesFragment(t *testing.T) {
	repo := &submissionRepository{}
	sender := &transactionalemail.MemorySender{}
	service := NewService(repo, ServiceOptions{
		Now:    func() time.Time { return time.Date(2026, 8, 22, 12, 0, 0, 0, time.UTC) },
		Tokens: securetokens.NewMemoryStore(), Email: sender,
		EmailFrom: "MycoOrigyn <notify@example.com>", ReviewerEmail: "reviewer@example.com",
		ReviewBaseURL: "https://mycoorigyn.com/early-access/review",
	})
	testingInterest := true
	input := SubmissionInput{SubmissionType: TypeEarlyAccess, Email: "owner@example.com", Name: "<Owner>", FarmName: "Farm & Fungi",
		FarmType: "Indoor", ProductionScale: "Pilot", CurrentTrackingMethod: "Spreadsheets",
		InterestedInTesting: &testingInterest, Message: `<img src=x onerror="alert(1)">`}
	if err := service.Submit(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	if err := service.Submit(context.Background(), input); err != nil {
		t.Fatal(err)
	}
	messages := sender.Snapshot()
	if len(repo.created) != 1 || len(messages) != 1 {
		t.Fatalf("created=%d messages=%d, want one each", len(repo.created), len(messages))
	}
	if !strings.Contains(messages[0].Text, "/early-access/review#token=") || strings.Contains(messages[0].Text, "?token=") {
		t.Fatalf("review URL is not fragment-based")
	}
	if strings.Contains(messages[0].HTML, "<Owner>") || strings.Contains(messages[0].HTML, "Farm & Fungi") || strings.Contains(messages[0].HTML, "<img") {
		t.Fatalf("review HTML did not escape dynamic content")
	}
	for _, expected := range []string{"<!doctype html>", "Internal review", "Review application", "Farm type", "Production scale", "Current tracking method", "Interested in testing", "Message", "Review link expires", "&lt;Owner&gt;", "&lt;img", "background-color:#06111f", "background-color:#42d8e8"} {
		if !strings.Contains(messages[0].HTML, expected) {
			t.Fatalf("branded review message is missing %q", expected)
		}
	}
	if strings.Count(messages[0].HTML, "#token=") != 3 || !strings.Contains(messages[0].Text, "Review link expires:") || messages[0].Subject == "" {
		t.Fatal("review token was not confined to intended URL placements or message context is incomplete")
	}
}

func TestWaitlistDoesNotCreateApprovalNotification(t *testing.T) {
	repo := &submissionRepository{}
	sender := &transactionalemail.MemorySender{}
	service := NewService(repo, ServiceOptions{Tokens: securetokens.NewMemoryStore(), Email: sender})
	if err := service.Submit(context.Background(), SubmissionInput{SubmissionType: TypeWaitlist, Email: "person@example.com"}); err != nil {
		t.Fatal(err)
	}
	if len(repo.created) != 1 || len(sender.Snapshot()) != 0 {
		t.Fatalf("waitlist unexpectedly entered approval workflow")
	}
}

func TestNotificationFailureKeepsRequestAndDoesNotLogToken(t *testing.T) {
	repo := &submissionRepository{}
	sender := &transactionalemail.MemorySender{Err: errors.New("provider body with secret")}
	var logs bytes.Buffer
	service := NewService(repo, ServiceOptions{
		Tokens: securetokens.NewMemoryStore(), Email: sender,
		EmailFrom: "notify@example.com", ReviewerEmail: "reviewer@example.com",
		ReviewBaseURL: "https://mycoorigyn.com/early-access/review",
		Logger:        slog.New(slog.NewJSONHandler(&logs, nil)),
	})
	if err := service.Submit(context.Background(), SubmissionInput{SubmissionType: TypeEarlyAccess, Email: "owner@example.com"}); err != nil {
		t.Fatal(err)
	}
	if len(repo.created) != 1 {
		t.Fatalf("durable request lost after notification failure")
	}
	if strings.Contains(logs.String(), "provider body") || strings.Contains(logs.String(), "#token=") {
		t.Fatalf("sensitive delivery information reached logs: %s", logs.String())
	}
}

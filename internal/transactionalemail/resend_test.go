package transactionalemail

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestResendSenderSuccessAndHeaders(t *testing.T) {
	var authorization string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		authorization = r.Header.Get("Authorization")
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Fatal(err)
		}
		if payload["subject"] != "Subject" || payload["text"] != "Text" || payload["html"] != "<p>HTML</p>" {
			t.Fatalf("unexpected payload: %#v", payload)
		}
		w.WriteHeader(http.StatusCreated)
	}))
	defer server.Close()

	sender := newTestResendSender(t, server.URL, time.Second)
	err := sender.Send(context.Background(), Message{
		To: "person@example.com", From: "MycoOrigyn <notify@example.com>",
		Subject: "Subject", Text: "Text", HTML: "<p>HTML</p>",
	})
	if err != nil {
		t.Fatal(err)
	}
	if authorization != "Bearer test-secret-key" {
		t.Fatal("authorization header not set")
	}
}

func TestResendSenderBoundsProviderErrors(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusBadRequest)
		_, _ = w.Write([]byte(`{"secret":"provider detail"}`))
	}))
	defer server.Close()

	err := newTestResendSender(t, server.URL, time.Second).Send(context.Background(), Message{})
	if err == nil || strings.Contains(err.Error(), "provider detail") || !strings.Contains(err.Error(), "400") {
		t.Fatalf("unsafe or missing bounded error: %v", err)
	}
}

func TestResendSenderHonorsContextCancellation(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		<-r.Context().Done()
	}))
	defer server.Close()

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := newTestResendSender(t, server.URL, time.Second).Send(ctx, Message{}); !errors.Is(err, context.Canceled) {
		t.Fatalf("error = %v, want context canceled", err)
	}
}

func TestResendSenderRejectsMissingOrUnsafeKeyFile(t *testing.T) {
	if _, err := NewResendSender(filepath.Join(t.TempDir(), "missing"), "", time.Second); err == nil {
		t.Fatal("expected missing key error")
	}
	path := filepath.Join(t.TempDir(), "key")
	if err := os.WriteFile(path, []byte("secret"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, err := NewResendSender(path, "", time.Second); err == nil {
		t.Fatal("expected unsafe key mode error")
	}
}

func newTestResendSender(t *testing.T, endpoint string, timeout time.Duration) *ResendSender {
	t.Helper()
	path := filepath.Join(t.TempDir(), "resend-key")
	if err := os.WriteFile(path, []byte("test-secret-key\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	sender, err := NewResendSender(path, endpoint, timeout)
	if err != nil {
		t.Fatal(err)
	}
	return sender
}

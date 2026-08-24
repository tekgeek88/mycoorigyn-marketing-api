package transactionalemail

import (
	"context"
	"errors"
	"testing"
)

func TestAllowlistSenderPermitsOnlyConfiguredRecipients(t *testing.T) {
	next := &MemorySender{}
	sender, err := NewAllowlistSender(next, []string{"reviewer@example.com", "tester@example.com"})
	if err != nil {
		t.Fatalf("create allowlist sender: %v", err)
	}

	if err := sender.Send(context.Background(), Message{To: "Reviewer@Example.com"}); err != nil {
		t.Fatalf("send allowed recipient: %v", err)
	}
	if err := sender.Send(context.Background(), Message{To: "grower@example.com"}); !errors.Is(err, ErrRecipientNotAllowed) {
		t.Fatalf("send disallowed recipient error = %v, want ErrRecipientNotAllowed", err)
	}
	if got := len(next.Snapshot()); got != 1 {
		t.Fatalf("delivered messages = %d, want 1", got)
	}
}

func TestAllowlistSenderRejectsInvalidConfiguration(t *testing.T) {
	if _, err := NewAllowlistSender(&MemorySender{}, nil); err == nil {
		t.Fatal("expected empty allowlist to fail")
	}
	if _, err := NewAllowlistSender(&MemorySender{}, []string{"not-an-email"}); err == nil {
		t.Fatal("expected invalid allowlist recipient to fail")
	}
}

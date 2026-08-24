package transactionalemail

import (
	"context"
	"errors"
	"net/mail"
	"strings"
)

var ErrRecipientNotAllowed = errors.New("transactional email recipient is not allowed")

type AllowlistSender struct {
	next    Sender
	allowed map[string]struct{}
}

func NewAllowlistSender(next Sender, recipients []string) (*AllowlistSender, error) {
	if next == nil || len(recipients) == 0 {
		return nil, errors.New("transactional email allowlist requires a sender and at least one recipient")
	}
	allowed := make(map[string]struct{}, len(recipients))
	for _, recipient := range recipients {
		recipient = strings.ToLower(strings.TrimSpace(recipient))
		address, err := mail.ParseAddress(recipient)
		if err != nil || address.Address != recipient {
			return nil, errors.New("transactional email allowlist contains an invalid recipient")
		}
		allowed[recipient] = struct{}{}
	}
	return &AllowlistSender{next: next, allowed: allowed}, nil
}

func (s *AllowlistSender) Send(ctx context.Context, message Message) error {
	recipient := strings.ToLower(strings.TrimSpace(message.To))
	address, err := mail.ParseAddress(recipient)
	if err != nil || address.Address != recipient {
		return ErrRecipientNotAllowed
	}
	if _, ok := s.allowed[recipient]; !ok {
		return ErrRecipientNotAllowed
	}
	return s.next.Send(ctx, message)
}

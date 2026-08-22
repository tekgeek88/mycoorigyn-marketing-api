package transactionalemail

import (
	"context"
	"errors"
	"sync"
)

var ErrDisabled = errors.New("transactional email delivery is disabled")

type Message struct {
	To      string
	From    string
	ReplyTo string
	Subject string
	Text    string
	HTML    string
}

type Sender interface {
	Send(ctx context.Context, message Message) error
}

type DisabledSender struct{}

func (DisabledSender) Send(context.Context, Message) error {
	return ErrDisabled
}

type MemorySender struct {
	mu       sync.Mutex
	Messages []Message
	Err      error
}

func (s *MemorySender) Send(_ context.Context, message Message) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.Err != nil {
		return s.Err
	}
	s.Messages = append(s.Messages, message)
	return nil
}

func (s *MemorySender) Snapshot() []Message {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]Message(nil), s.Messages...)
}

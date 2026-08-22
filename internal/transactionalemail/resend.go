package transactionalemail

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const defaultResendEndpoint = "https://api.resend.com/emails"

type ResendSender struct {
	apiKey   string
	endpoint string
	client   *http.Client
}

func NewResendSender(apiKeyFile, endpoint string, timeout time.Duration) (*ResendSender, error) {
	info, err := os.Lstat(apiKeyFile)
	if err != nil {
		return nil, errors.New("read Resend API key configuration")
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return nil, errors.New("Resend API key file must be a private regular file")
	}
	body, err := os.ReadFile(apiKeyFile)
	if err != nil {
		return nil, errors.New("read Resend API key configuration")
	}
	apiKey := strings.TrimSpace(string(body))
	if apiKey == "" || strings.ContainsAny(apiKey, "\r\n") {
		return nil, errors.New("Resend API key file is empty or malformed")
	}
	if strings.TrimSpace(endpoint) == "" {
		endpoint = defaultResendEndpoint
	}
	if timeout <= 0 {
		timeout = 10 * time.Second
	}
	return &ResendSender{
		apiKey:   apiKey,
		endpoint: endpoint,
		client:   &http.Client{Timeout: timeout},
	}, nil
}

func (s *ResendSender) Send(ctx context.Context, message Message) error {
	payload := struct {
		From    string   `json:"from"`
		To      []string `json:"to"`
		ReplyTo string   `json:"reply_to,omitempty"`
		Subject string   `json:"subject"`
		HTML    string   `json:"html"`
		Text    string   `json:"text"`
	}{
		From:    message.From,
		To:      []string{message.To},
		ReplyTo: message.ReplyTo,
		Subject: message.Subject,
		HTML:    message.HTML,
		Text:    message.Text,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return errors.New("encode transactional email request")
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return errors.New("create transactional email request")
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return err
		}
		return errors.New("transactional email provider request failed")
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("transactional email provider returned status %d", resp.StatusCode)
	}
	return nil
}

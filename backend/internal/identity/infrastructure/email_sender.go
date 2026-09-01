package infrastructure

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"time"

	"muse-backend/internal/identity/domain"
)

type EmailContent struct {
	Subject string
	Text    string
}

type emailTemplates struct {
	baseURL string
}

func (t emailTemplates) verification(token string) EmailContent {
	return EmailContent{
		Subject: "Verify your Muse email address",
		Text: "Welcome to Muse.\n\n" +
			"Open this link to verify your email address and finish creating your account:\n\n" +
			t.link("/auth/verify-email", token) + "\n\n" +
			"The link works once and expires in 24 hours.\n\n" +
			"If you didn't try to create a Muse account, you can ignore this email.\n",
	}
}

func (t emailTemplates) passwordReset(token string) EmailContent {
	return EmailContent{
		Subject: "Reset your Muse password",
		Text: "Someone asked to reset the password for this Muse account.\n\n" +
			"Open this link to choose a new one:\n\n" +
			t.link("/auth/reset-password", token) + "\n\n" +
			"The link works once and expires in 1 hour. " +
			"Resetting your password signs you out everywhere.\n\n" +
			"If this wasn't you, you can ignore this email — nothing has changed.\n",
	}
}

func (t emailTemplates) signupOnExistingAccount() EmailContent {
	return EmailContent{
		Subject: "You already have a Muse account",
		Text: "Someone tried to create a Muse account with this email address, " +
			"but one already exists.\n\n" +
			"If that was you, log in instead. If you've forgotten your password, " +
			"use \"Forgot Password?\" on the log-in screen.\n\n" +
			"If this wasn't you, you can ignore this email — nothing has changed.\n",
	}
}

func (t emailTemplates) link(path, token string) string {
	base := strings.TrimRight(t.baseURL, "/")
	if base == "" {
		base = "https://muse.app"
	}
	return base + path + "?token=" + url.QueryEscape(token)
}

type ResendEmailSender struct {
	apiKey    string
	from      string
	templates emailTemplates
	client    *http.Client
	endpoint  string
}

const ResendEndpoint = "https://api.resend.com/emails"

func NewResendEmailSender(apiKey, from, baseURL string, client *http.Client) *ResendEmailSender {
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	return &ResendEmailSender{
		apiKey:    apiKey,
		from:      from,
		templates: emailTemplates{baseURL: baseURL},
		client:    client,
		endpoint:  ResendEndpoint,
	}
}

func (s *ResendEmailSender) SetEndpoint(endpoint string) { s.endpoint = endpoint }

func (s *ResendEmailSender) SendEmailVerification(ctx context.Context, to domain.EmailAddress, token string) error {
	return s.send(ctx, to, s.templates.verification(token))
}

func (s *ResendEmailSender) SendPasswordReset(ctx context.Context, to domain.EmailAddress, token string) error {
	return s.send(ctx, to, s.templates.passwordReset(token))
}

func (s *ResendEmailSender) SendSignupOnExistingAccount(ctx context.Context, to domain.EmailAddress) error {
	return s.send(ctx, to, s.templates.signupOnExistingAccount())
}

type resendRequest struct {
	From    string   `json:"from"`
	To      []string `json:"to"`
	Subject string   `json:"subject"`
	Text    string   `json:"text"`
}

func (s *ResendEmailSender) send(ctx context.Context, to domain.EmailAddress, content EmailContent) error {
	body, err := json.Marshal(resendRequest{
		From:    s.from,
		To:      []string{to.String()},
		Subject: content.Subject,
		Text:    content.Text,
	})
	if err != nil {
		return fmt.Errorf("resend: encode request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, s.endpoint, bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("resend: build request: %w", err)
	}
	req.Header.Set("Authorization", "Bearer "+s.apiKey)
	req.Header.Set("Content-Type", "application/json")

	resp, err := s.client.Do(req)
	if err != nil {
		return fmt.Errorf("resend: send: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return fmt.Errorf("resend: unexpected status %d", resp.StatusCode)
	}
	return nil
}

type LogEmailSender struct {
	logger    *slog.Logger
	templates emailTemplates

	lastToken string
}

func NewLogEmailSender(logger *slog.Logger, baseURL string) *LogEmailSender {
	return &LogEmailSender{logger: logger, templates: emailTemplates{baseURL: baseURL}}
}

func (s *LogEmailSender) SendEmailVerification(_ context.Context, to domain.EmailAddress, token string) error {
	s.lastToken = token
	s.logger.Warn("non-production email sender: verification email not actually sent",
		"recipient_domain", domainOf(to))
	return nil
}

func (s *LogEmailSender) SendPasswordReset(_ context.Context, to domain.EmailAddress, token string) error {
	s.lastToken = token
	s.logger.Warn("non-production email sender: password reset email not actually sent",
		"recipient_domain", domainOf(to))
	return nil
}

func (s *LogEmailSender) SendSignupOnExistingAccount(_ context.Context, to domain.EmailAddress) error {
	s.logger.Warn("non-production email sender: existing-account notice not actually sent",
		"recipient_domain", domainOf(to))
	return nil
}

func (s *LogEmailSender) LastTokenForLocalDevelopment() string { return s.lastToken }

func domainOf(address domain.EmailAddress) string {
	at := strings.LastIndexByte(address.String(), '@')
	if at < 0 || at+1 >= len(address) {
		return "unknown"
	}
	return address.String()[at+1:]
}

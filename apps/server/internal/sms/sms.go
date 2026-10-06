// Package sms delivers one-time codes. The default provider logs instead of
// sending, so the app runs with no third-party account; Twilio is used when it
// is configured.
package sms

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"strings"
	"time"

	"gamematch/internal/config"
)

// Provider sends a text message.
type Provider interface {
	Send(ctx context.Context, to, body string) error
}

// LogProvider writes the message to the log. It is the default so a local or
// self-hosted deploy needs no SMS account.
type LogProvider struct{}

// Send logs the code.
func (LogProvider) Send(_ context.Context, to, body string) error {
	log.Printf("sms: to=%s body=%q (no SMS provider configured)", maskPhone(to), body)
	return nil
}

// TwilioProvider sends through the Twilio REST API.
type TwilioProvider struct {
	AccountSID string
	AuthToken  string
	From       string
	HTTP       *http.Client
}

// Send posts the message to Twilio.
func (p TwilioProvider) Send(ctx context.Context, to, body string) error {
	client := p.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	form := url.Values{}
	form.Set("To", to)
	form.Set("From", p.From)
	form.Set("Body", body)

	endpoint := fmt.Sprintf("https://api.twilio.com/2010-04-01/Accounts/%s/Messages.json", p.AccountSID)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, strings.NewReader(form.Encode()))
	if err != nil {
		return err
	}
	req.SetBasicAuth(p.AccountSID, p.AuthToken)
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode >= 300 {
		var payload map[string]any
		_ = json.NewDecoder(resp.Body).Decode(&payload)
		return fmt.Errorf("twilio: status %d: %v", resp.StatusCode, payload["message"])
	}
	return nil
}

// New picks a provider from configuration, falling back to logging.
func New(cfg config.Config) Provider {
	if cfg.SMSProvider == "twilio" &&
		cfg.TwilioAccountID != "" && cfg.TwilioAuthToken != "" && cfg.TwilioFrom != "" {
		return TwilioProvider{
			AccountSID: cfg.TwilioAccountID,
			AuthToken:  cfg.TwilioAuthToken,
			From:       cfg.TwilioFrom,
		}
	}
	return LogProvider{}
}

// maskPhone keeps only the last two digits so codes are not tied to a number in
// the log.
func maskPhone(p string) string {
	if len(p) <= 2 {
		return "**"
	}
	return strings.Repeat("*", len(p)-2) + p[len(p)-2:]
}

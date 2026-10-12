// Package sms delivers one-time codes. The default provider logs instead of
// sending, so the app runs with no third-party account; Twilio is used when it
// is configured.
package sms

import (
	"context"
	"encoding/json"
	"errors"
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

// Send records that a message was produced. The body is deliberately never
// logged: it contains the one-time code, and the journal is readable by anyone
// in the systemd-journal group and by any log shipper.
func (LogProvider) Send(_ context.Context, to, body string) error {
	log.Printf("sms: to=%s chars=%d (no SMS provider configured)", maskPhone(to), len(body))
	return nil
}

// ErrNoProvider reports that no provider can deliver a message.
var ErrNoProvider = errors.New("sms: no provider configured")

// FatalLogProvider refuses to send. It stands in for a missing provider on live
// traffic, where silently dropping codes would strand every user trying to sign
// in.
type FatalLogProvider struct{}

// Send always fails without recording the body.
func (FatalLogProvider) Send(_ context.Context, to, _ string) error {
	log.Printf("sms: to=%s rejected (no SMS provider configured on this deployment)", maskPhone(to))
	return ErrNoProvider
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

// New picks a provider from configuration, falling back to logging outside
// production. Callers serving live traffic should use NewChecked.
func New(cfg config.Config) Provider {
	p, _ := NewChecked(cfg)
	return p
}

// NewChecked picks a provider and refuses to return a log-only provider on
// live traffic, so a half-configured Twilio account fails at boot instead of
// writing one-time codes to the journal.
func NewChecked(cfg config.Config) (Provider, error) {
	if cfg.SMSProvider == "twilio" &&
		cfg.TwilioAccountID != "" && cfg.TwilioAuthToken != "" && cfg.TwilioFrom != "" {
		return TwilioProvider{
			AccountSID: cfg.TwilioAccountID,
			AuthToken:  cfg.TwilioAuthToken,
			From:       cfg.TwilioFrom,
		}, nil
	}
	if cfg.IsProduction() {
		return FatalLogProvider{}, fmt.Errorf(
			"%w: SMS_PROVIDER=%q with account=%t token=%t from=%t; set a working provider before serving traffic",
			ErrNoProvider, cfg.SMSProvider,
			cfg.TwilioAccountID != "", cfg.TwilioAuthToken != "", cfg.TwilioFrom != "")
	}
	return LogProvider{}, nil
}

// maskPhone keeps only the last two digits so codes are not tied to a number in
// the log.
func maskPhone(p string) string {
	if len(p) <= 2 {
		return "**"
	}
	return strings.Repeat("*", len(p)-2) + p[len(p)-2:]
}

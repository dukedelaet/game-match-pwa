// Package turnstile verifies Cloudflare Turnstile challenges. Without a secret
// it accepts everything, so local and self-hosted deploys are unaffected.
package turnstile

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"strings"
	"time"
)

// Verifier checks a challenge token.
type Verifier interface {
	Verify(ctx context.Context, token, remoteIP string) (bool, error)
}

// Noop accepts every token. Used when Turnstile is not configured.
type Noop struct{}

// Verify always succeeds.
func (Noop) Verify(context.Context, string, string) (bool, error) { return true, nil }

// Cloudflare calls the Turnstile siteverify endpoint.
type Cloudflare struct {
	Secret string
	HTTP   *http.Client
}

// Verify posts the token to Cloudflare.
func (c Cloudflare) Verify(ctx context.Context, token, remoteIP string) (bool, error) {
	if token == "" {
		return false, nil
	}
	client := c.HTTP
	if client == nil {
		client = &http.Client{Timeout: 10 * time.Second}
	}
	form := url.Values{}
	form.Set("secret", c.Secret)
	form.Set("response", token)
	if remoteIP != "" {
		form.Set("remoteip", remoteIP)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost,
		"https://challenges.cloudflare.com/turnstile/v0/siteverify",
		strings.NewReader(form.Encode()))
	if err != nil {
		return false, err
	}
	req.Header.Set("Content-Type", "application/x-www-form-urlencoded")

	resp, err := client.Do(req)
	if err != nil {
		return false, err
	}
	defer func() { _ = resp.Body.Close() }()

	var payload struct {
		Success bool `json:"success"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&payload); err != nil {
		return false, err
	}
	return payload.Success, nil
}

// New returns a Cloudflare verifier when a secret is set, else a no-op.
func New(secret string) Verifier {
	if secret == "" {
		return Noop{}
	}
	return Cloudflare{Secret: secret}
}

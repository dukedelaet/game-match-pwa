// Package push sends Web Push notifications. Without VAPID keys the sender is
// inert: subscriptions are still stored, but sends are skipped and logged.
package push

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"time"

	webpush "github.com/SherClockHolmes/webpush-go"

	"gamematch/internal/config"
)

// Subscription is the browser side of a push channel.
type Subscription struct {
	Endpoint string
	P256dh   string
	Auth     string
}

// Message is the notification payload delivered to the service worker.
type Message struct {
	Title string `json:"title"`
	Body  string `json:"body"`
	URL   string `json:"url,omitempty"`
	Tag   string `json:"tag,omitempty"`
}

// Sender delivers messages to subscriptions.
type Sender struct {
	publicKey  string
	privateKey string
	subject    string
	client     *http.Client
}

// New builds a sender from configuration. Enabled reports whether VAPID keys
// are present.
func New(cfg config.Config) *Sender {
	return &Sender{
		publicKey:  cfg.VAPIDPublicKey,
		privateKey: cfg.VAPIDPrivateKey,
		subject:    cfg.VAPIDSubject,
		client:     &http.Client{Timeout: 10 * time.Second},
	}
}

// Enabled reports whether push can actually be delivered.
func (s *Sender) Enabled() bool {
	return s != nil && s.publicKey != "" && s.privateKey != ""
}

// PublicKey returns the VAPID public key the browser needs, or "".
func (s *Sender) PublicKey() string {
	if s == nil {
		return ""
	}
	return s.publicKey
}

// Send delivers a message to every subscription. It is best-effort and never
// blocks the caller: deliveries run in the background on a context detached
// from the request that triggered them.
func (s *Sender) Send(ctx context.Context, subscriptions []Subscription, message Message) {
	if !s.Enabled() || len(subscriptions) == 0 {
		return
	}
	payload, err := json.Marshal(message)
	if err != nil {
		log.Printf("push encode: %v", err)
		return
	}
	base := context.WithoutCancel(ctx)
	for _, sub := range subscriptions {
		go s.deliver(base, sub, payload)
	}
}

func (s *Sender) deliver(ctx context.Context, sub Subscription, payload []byte) {
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()

	options := &webpush.Options{
		Subscriber:      s.subject,
		VAPIDPublicKey:  s.publicKey,
		VAPIDPrivateKey: s.privateKey,
		TTL:             60,
		HTTPClient:      s.client,
	}
	response, err := webpush.SendNotificationWithContext(ctx, payload, &webpush.Subscription{
		Endpoint: sub.Endpoint,
		Keys:     webpush.Keys{P256dh: sub.P256dh, Auth: sub.Auth},
	}, options)
	if err != nil {
		log.Printf("push send: %v", err)
		return
	}
	defer func() { _ = response.Body.Close() }()
	// 404/410 mean the subscription is gone; the client prunes on next sign-in.
	if response.StatusCode >= 400 && response.StatusCode != http.StatusNotFound && response.StatusCode != http.StatusGone {
		log.Printf("push send: status %d", response.StatusCode)
	}
}

// GenerateKeys creates a VAPID key pair for the operator to put in the env file.
func GenerateKeys() (privateKey, publicKey string, err error) {
	return webpush.GenerateVAPIDKeys()
}

// Package push sends Web Push notifications. Without VAPID keys the sender is
// inert: subscriptions are still stored, but sends are skipped and logged.
package push

import (
	"context"
	"encoding/json"
	"log"
	"net/http"
	"sync"
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

// Pruner removes a subscription that the push service reports as gone.
type Pruner interface {
	PruneSubscription(ctx context.Context, endpoint string)
}

// Sender delivers messages to subscriptions.
type Sender struct {
	publicKey  string
	privateKey string
	subject    string
	client     *http.Client
	// pruner removes dead endpoints. Without one, a subscription that the push
	// service has dropped is retried on every notification forever.
	pruner Pruner

	// deliver performs one delivery. Defaults to deliverOne; tests substitute it.
	deliver func(ctx context.Context, sub Subscription, payload []byte)
}

// send performs one delivery through the configured path.
func (s *Sender) send(ctx context.Context, sub Subscription, payload []byte) {
	if s.deliver != nil {
		s.deliver(ctx, sub, payload)
		return
	}
	s.deliverOne(ctx, sub, payload)
}

// maxConcurrentDeliveries caps fan-out. One goroutine per subscription with no
// ceiling meant a user who registered many endpoints could spawn unbounded
// concurrent requests, each holding a connection open for its timeout.
const maxConcurrentDeliveries = 16

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
	s.fanOut(base, subscriptions, payload)
}

// fanOut delivers to every subscription, with a bounded number in flight.
func (s *Sender) fanOut(ctx context.Context, subscriptions []Subscription, payload []byte) {
	if len(subscriptions) == 0 {
		return
	}
	limit := maxConcurrentDeliveries
	if len(subscriptions) < limit {
		limit = len(subscriptions)
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup

	for _, sub := range subscriptions {
		wg.Add(1)
		go func(sub Subscription) {
			defer wg.Done()
			select {
			case sem <- struct{}{}:
				defer func() { <-sem }()
			case <-ctx.Done():
				return
			}
			s.send(ctx, sub, payload)
		}(sub)
	}
	wg.Wait()
}

// deliverOne sends to a single subscription and prunes it if the push service
// says the endpoint is gone.
func (s *Sender) deliverOne(ctx context.Context, sub Subscription, payload []byte) {
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

	// 404 and 410 mean the subscription is gone for good. The previous comment
	// here claimed the client pruned on next sign-in, but nothing ever did:
	// unsubscribing was the only delete path, so dead endpoints accumulated and
	// every notification kept paying for a doomed request.
	s.handleStatus(ctx, sub, response.StatusCode)
}

// handleStatus decides what a push service response means for a subscription.
// Split out from deliverOne so the policy can be tested without a valid VAPID
// keypair, which webpush requires before it will even make a request.
func (s *Sender) handleStatus(ctx context.Context, sub Subscription, status int) {
	switch status {
	case http.StatusNotFound, http.StatusGone:
		log.Printf("push send: subscription gone, pruning %s", sub.Endpoint)
		if s.pruner != nil {
			s.pruner.PruneSubscription(context.WithoutCancel(ctx), sub.Endpoint)
		}
	default:
		if status >= 400 {
			// Transient: keep the subscription and try again next time.
			log.Printf("push send: status %d", status)
		}
	}
}

// GenerateKeys creates a VAPID key pair for the operator to put in the env file.
func GenerateKeys() (privateKey, publicKey string, err error) {
	return webpush.GenerateVAPIDKeys()
}

// WithPruner builds a sender that removes dead endpoints. Dead subscriptions
// otherwise accumulate forever, since unsubscribing was the only delete path.
func WithPruner(cfg config.Config, p Pruner) *Sender {
	s := New(cfg)
	if s != nil {
		s.pruner = p
	}
	return s
}

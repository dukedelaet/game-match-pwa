package push

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strconv"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gamematch/internal/config"
)

func TestSenderIsDisabledWithoutKeys(t *testing.T) {
	sender := New(config.Config{})
	require.False(t, sender.Enabled())
	require.Empty(t, sender.PublicKey())

	// Sending is a no-op, not a panic.
	require.NotPanics(t, func() {
		sender.Send(context.Background(), []Subscription{{Endpoint: "https://push.example/x"}}, Message{Title: "hi"})
	})
}

func TestSenderEnabledWithKeys(t *testing.T) {
	sender := New(config.Config{VAPIDPublicKey: "pub", VAPIDPrivateKey: "priv"})
	require.True(t, sender.Enabled())
	require.Equal(t, "pub", sender.PublicKey())
}

func TestGenerateKeysProducesAPair(t *testing.T) {
	privateKey, publicKey, err := GenerateKeys()
	require.NoError(t, err)
	require.NotEmpty(t, privateKey)
	require.NotEmpty(t, publicKey)
	require.NotEqual(t, privateKey, publicKey)
}

func TestSendWithNoSubscriptionsDoesNothing(t *testing.T) {
	sender := New(config.Config{VAPIDPublicKey: "pub", VAPIDPrivateKey: "priv"})
	require.NotPanics(t, func() {
		sender.Send(context.Background(), nil, Message{Title: "hi"})
	})
}

// The comment in deliver claimed "the client prunes on next sign-in". Nothing
// did: the only delete path is an explicit unsubscribe, so dead endpoints
// accumulated forever and every notification paid for more doomed TLS
// handshakes.
func TestGoneSubscriptionIsPruned(t *testing.T) {
	var got []string
	remover := &recordingPruner{removed: &got}

	sender := &Sender{
		publicKey:  "k",
		privateKey: "p",
		subject:    "mailto:ops@example.com",
		client:     serverClient(t, http.StatusGone),
		pruner:     remover,
	}

	sender.handleStatus(context.Background(), Subscription{Endpoint: "https://push.example/gone"}, http.StatusGone)

	require.Equal(t, []string{"https://push.example/gone"}, got,
		"a 410 response means the endpoint is gone and must be removed")
}

// A transient failure must NOT prune: the subscription is probably still good.
func TestTransientFailureDoesNotPrune(t *testing.T) {
	var got []string
	remover := &recordingPruner{removed: &got}

	sender := &Sender{
		publicKey:  "k",
		privateKey: "p",
		subject:    "mailto:ops@example.com",
		client:     serverClient(t, http.StatusServiceUnavailable),
		pruner:     remover,
	}

	sender.handleStatus(context.Background(), Subscription{Endpoint: "https://push.example/flaky"}, http.StatusServiceUnavailable)

	require.Empty(t, got, "a 503 is transient; the subscription must be kept")
}

// Fan-out must be bounded: one goroutine per subscription with no cap means a
// user who registers many endpoints can spawn unbounded concurrent requests.
func TestFanOutIsBounded(t *testing.T) {
	var inFlight int32
	var peak int32
	var mu sync.Mutex
	unblock := make(chan struct{})

	sender := &Sender{
		publicKey: "k", privateKey: "p", subject: "mailto:ops@example.com",
		deliver: func(ctx context.Context, sub Subscription, payload []byte) {
			n := atomic.AddInt32(&inFlight, 1)
			mu.Lock()
			if n > peak {
				peak = n
			}
			mu.Unlock()
			<-unblock
			atomic.AddInt32(&inFlight, -1)
		},
	}

	var subs []Subscription
	for i := 0; i < 100; i++ {
		subs = append(subs, Subscription{Endpoint: "https://push.example/" + strconv.Itoa(i)})
	}

	done := make(chan struct{})
	go func() { sender.fanOut(context.Background(), subs, []byte(`{}`)); close(done) }()

	// Wait until the cap is saturated, then check how many are in flight.
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if atomic.LoadInt32(&inFlight) >= maxConcurrentDeliveries {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	got := int(atomic.LoadInt32(&inFlight))
	close(unblock)
	<-done

	require.Equal(t, maxConcurrentDeliveries, got,
		"fan-out should run exactly the cap at once, not %d", got)
	require.Less(t, got, 100, "fan-out must be capped")
}

type recordingPruner struct{ removed *[]string }

func (r *recordingPruner) PruneSubscription(_ context.Context, endpoint string) {
	*r.removed = append(*r.removed, endpoint)
}

func serverClient(t *testing.T, status int) *http.Client {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(status)
	}))
	t.Cleanup(srv.Close)
	return srv.Client()
}

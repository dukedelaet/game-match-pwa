package push

import (
	"context"
	"testing"

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

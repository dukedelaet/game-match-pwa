package game

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gamematch/internal/store"
)

func TestRunTickerAdvancesSessionsAndExpiresInvites(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	alex := userID(t, st, "Alex")
	jordan := userID(t, st, "Jordan")
	ses, err := e.StartSession(ctx, "this_or_that", "queue_1v1", []string{alex, jordan})
	require.NoError(t, err)
	require.NoError(t, e.Join(ctx, ses.ID, alex))
	require.NoError(t, e.Join(ctx, ses.ID, jordan))

	// move the countdown into the past without touching the answer deadline
	past := store.FmtTS(time.Now().UTC().Add(-time.Second))
	_, err = st.DB.Exec(`UPDATE game_sessions SET started_at=? WHERE id=?`, past, ses.ID)
	require.NoError(t, err)

	// an expired pending invite for the ticker to sweep
	now := store.NowTS()
	_, err = st.DB.Exec(`INSERT INTO invites (id,from_user_id,to_user_id,game_kind,state,expires_at,created_at,updated_at)
		VALUES (?,?,?,?,?,?,?,?)`, "inv-expired", alex, jordan, "this_or_that", "pending",
		store.FmtTS(time.Now().UTC().Add(-time.Minute)), now, now)
	require.NoError(t, err)

	go e.RunTicker(ctx, 20*time.Millisecond)

	require.Eventually(t, func() bool {
		got, err := st.GetSession(context.Background(), ses.ID)
		return err == nil && got.State == "in_round"
	}, 2*time.Second, 10*time.Millisecond)

	inv, err := st.GetInvite(context.Background(), "inv-expired")
	require.NoError(t, err)
	require.Equal(t, "expired", inv.State)
}

package game

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gamematch/internal/store"
)

func TestMatcherAdjacentMetroExpansionAfter32s(t *testing.T) {
	_, st, cfg := newEngineRig(t)
	ctx := context.Background()
	m := NewMatcher(st, cfg)

	la := metroID(t, st, "los-angeles")
	ny := metroID(t, st, "new-york")
	alex := userID(t, st, "Alex")
	jordan := userID(t, st, "Jordan")

	// Make the metros adjacent and move Jordan to New York.
	_, err := st.DB.Exec(`UPDATE metros SET adjacent_ids=? WHERE id=?`, `["`+ny+`"]`, la)
	require.NoError(t, err)
	_, err = st.DB.Exec(`UPDATE users SET metro_id=? WHERE id=?`, ny, jordan)
	require.NoError(t, err)

	require.NoError(t, m.Enqueue(ctx, alex, "this_or_that", false))
	require.NoError(t, m.Enqueue(ctx, jordan, "this_or_that", false))

	// Immediately: only same-metro candidates are considered.
	status, err := m.Tick(ctx, alex)
	require.NoError(t, err)
	require.Equal(t, "waiting", status.State)

	// After 32s the matcher widens to adjacent metros.
	_, err = st.DB.Exec(`UPDATE queue_entries SET enqueued_at=? WHERE user_id=?`,
		store.FmtTS(time.Now().UTC().Add(-33*time.Second)), alex)
	require.NoError(t, err)

	status, err = m.Tick(ctx, alex)
	require.NoError(t, err)
	require.Equal(t, "matched", status.State)

	session, err := st.GetSession(ctx, status.SessionID)
	require.NoError(t, err)
	require.Equal(t, "queue_1v1", session.Mode)
	parts, err := st.Participants(ctx, status.SessionID)
	require.NoError(t, err)
	require.Len(t, parts, 2)
}

func TestMatcherPracticeFallbackAfter30s(t *testing.T) {
	_, st, cfg := newEngineRig(t)
	ctx := context.Background()
	m := NewMatcher(st, cfg)
	alex := userID(t, st, "Alex")

	require.NoError(t, m.Enqueue(ctx, alex, "this_or_that", true))

	// Before the practice window: still waiting, no practice offered.
	status, err := m.Tick(ctx, alex)
	require.NoError(t, err)
	require.Equal(t, "waiting", status.State)
	require.False(t, status.CanPractice)

	// Past 30s with allowPractice: matched against the House.
	_, err = st.DB.Exec(`UPDATE queue_entries SET enqueued_at=? WHERE user_id=?`,
		store.FmtTS(time.Now().UTC().Add(-31*time.Second)), alex)
	require.NoError(t, err)

	status, err = m.Tick(ctx, alex)
	require.NoError(t, err)
	require.Equal(t, "matched", status.State)

	session, err := st.GetSession(ctx, status.SessionID)
	require.NoError(t, err)
	require.Equal(t, "practice", session.Mode)

	parts, err := st.Participants(ctx, status.SessionID)
	require.NoError(t, err)
	require.Len(t, parts, 2)
	ids := []string{parts[0].UserID, parts[1].UserID}
	require.Contains(t, ids, cfg.HouseUserID)
}

func TestMatcherSkipsBlockedCandidate(t *testing.T) {
	_, st, cfg := newEngineRig(t)
	ctx := context.Background()
	m := NewMatcher(st, cfg)
	alex := userID(t, st, "Alex")
	jordan := userID(t, st, "Jordan")

	require.NoError(t, st.InsertBlock(ctx, alex, jordan))
	require.NoError(t, m.Enqueue(ctx, alex, "this_or_that", false))
	require.NoError(t, m.Enqueue(ctx, jordan, "this_or_that", false))

	status, err := m.Tick(ctx, alex)
	require.NoError(t, err)
	require.Equal(t, "waiting", status.State, "a blocked pair must never match")

	// Unblocking lets them match.
	require.NoError(t, st.DeleteBlock(ctx, alex, jordan))
	status, err = m.Tick(ctx, alex)
	require.NoError(t, err)
	require.Equal(t, "matched", status.State)
}

func TestMatcherSkipsIntentMismatch(t *testing.T) {
	_, st, cfg := newEngineRig(t)
	ctx := context.Background()
	m := NewMatcher(st, cfg)
	alex := userID(t, st, "Alex")
	jordan := userID(t, st, "Jordan")

	// Alex wants dating; Jordan only wants friendship -> dating pool mismatch.
	require.NoError(t, st.ReplaceIntents(ctx, alex, []string{"dating"}))
	require.NoError(t, st.ReplaceIntents(ctx, jordan, []string{"friendship"}))

	require.NoError(t, m.Enqueue(ctx, alex, "this_or_that", false))
	require.NoError(t, m.Enqueue(ctx, jordan, "this_or_that", false))

	status, err := m.Tick(ctx, alex)
	require.NoError(t, err)
	require.Equal(t, "waiting", status.State)
}

func TestMatcherLeaveRemovesOnlyUnmatched(t *testing.T) {
	_, st, cfg := newEngineRig(t)
	ctx := context.Background()
	m := NewMatcher(st, cfg)
	alex := userID(t, st, "Alex")
	jordan := userID(t, st, "Jordan")

	// a waiting entry is removed
	require.NoError(t, m.Enqueue(ctx, alex, "this_or_that", false))
	require.NoError(t, m.Leave(ctx, alex))
	entry, err := st.QueueEntryForUser(ctx, alex)
	require.NoError(t, err)
	require.Nil(t, entry)

	// a matched entry is left alone (the session needs it)
	require.NoError(t, m.Enqueue(ctx, alex, "this_or_that", false))
	require.NoError(t, m.Enqueue(ctx, jordan, "this_or_that", false))
	status, err := m.Tick(ctx, alex)
	require.NoError(t, err)
	require.Equal(t, "matched", status.State)

	require.NoError(t, m.Leave(ctx, alex))
	entry, err = st.QueueEntryForUser(ctx, alex)
	require.NoError(t, err)
	require.NotNil(t, entry)
}

func TestMatcherIgnoredWhenIncognito(t *testing.T) {
	_, st, cfg := newEngineRig(t)
	ctx := context.Background()
	m := NewMatcher(st, cfg)
	alex := userID(t, st, "Alex")
	jordan := userID(t, st, "Jordan")

	_, err := st.DB.Exec(`UPDATE users SET incognito=1 WHERE id=?`, jordan)
	require.NoError(t, err)

	require.NoError(t, m.Enqueue(ctx, alex, "this_or_that", false))
	require.NoError(t, m.Enqueue(ctx, jordan, "this_or_that", false))

	status, err := m.Tick(ctx, alex)
	require.NoError(t, err)
	require.Equal(t, "waiting", status.State, "incognito users are hidden from the lobby")
}

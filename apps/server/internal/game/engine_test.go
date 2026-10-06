package game

import (
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gamematch/internal/config"
	"gamematch/internal/db"
	"gamematch/internal/seed"
	"gamematch/internal/store"
)

// newEngineRig builds a seeded temp database with an engine over it.
func newEngineRig(t *testing.T) (*Engine, *store.Store, config.Config) {
	t.Helper()
	dir := t.TempDir()
	conn, err := db.Open(filepath.Join(dir, "test.db"))
	require.NoError(t, err)
	cfg := config.Config{
		Addr:        "127.0.0.1:0",
		DataDir:     dir,
		Env:         "test",
		Debug:       true,
		HouseUserID: "00000000-0000-4000-8000-000000000001",
		DevOTP:      "123456",
	}
	require.NoError(t, db.Migrate(context.Background(), conn))
	st := store.New(conn, cfg)
	require.NoError(t, seed.Run(context.Background(), st, cfg))
	return NewEngine(st, cfg), st, cfg
}

func userID(t *testing.T, st *store.Store, name string) string {
	t.Helper()
	u, err := st.UserByName(context.Background(), name)
	require.NoError(t, err)
	return u.ID
}

func metroID(t *testing.T, st *store.Store, slug string) string {
	t.Helper()
	var id string
	err := st.DB.Get(&id, `SELECT id FROM metros WHERE slug = ?`, slug)
	require.NoError(t, err)
	return id
}

// backdate pushes the session's countdown and answer deadlines into the past so
// the next Advance moves the state machine without waiting on real timers.
func backdate(t *testing.T, st *store.Store, sessionID string) {
	t.Helper()
	past := store.FmtTS(time.Now().UTC().Add(-2 * time.Second))
	_, err := st.DB.Exec(`UPDATE game_sessions SET started_at=?, answer_by=? WHERE id=?`, past, past, sessionID)
	require.NoError(t, err)
	_, err = st.DB.Exec(`UPDATE rounds SET answer_by=? WHERE session_id=?`, past, sessionID)
	require.NoError(t, err)
}

func TestPracticeSessionVsHouse(t *testing.T) {
	e, st, cfg := newEngineRig(t)
	ctx := context.Background()
	alex := userID(t, st, "Alex")

	ses, err := e.StartSession(ctx, "this_or_that", "practice", []string{alex, cfg.HouseUserID})
	require.NoError(t, err)

	// practice needs only one human to join
	require.NoError(t, e.Join(ctx, ses.ID, alex))
	got, err := st.GetSession(ctx, ses.ID)
	require.NoError(t, err)
	require.Equal(t, "countdown", got.State)

	backdate(t, st, ses.ID)
	require.NoError(t, e.Advance(ctx, ses.ID))
	got, err = st.GetSession(ctx, ses.ID)
	require.NoError(t, err)
	require.Equal(t, "in_round", got.State)

	// the House auto-answers the opposite side
	require.NoError(t, e.Answer(ctx, ses.ID, alex, json.RawMessage(`{"choice":"right"}`)))
	v, err := e.View(ctx, ses.ID, alex)
	require.NoError(t, err)
	require.Equal(t, "reveal_round", v.State)
	require.NotNil(t, v.Opponent)
	require.Equal(t, "house", v.Opponent.ID)
	require.Equal(t, "House", *v.Opponent.Name)
	require.False(t, v.Reveal.Same, "Alex picked right, House always picks left")

	// play out the remaining rounds (2..8)
	for round := 2; round <= 8; round++ {
		backdate(t, st, ses.ID)
		require.NoError(t, e.Advance(ctx, ses.ID))
		got, err = st.GetSession(ctx, ses.ID)
		require.NoError(t, err)
		require.Equal(t, "in_round", got.State, "round %d", round)
		require.NoError(t, e.Answer(ctx, ses.ID, alex, json.RawMessage(`{"choice":"right"}`)))
	}
	backdate(t, st, ses.ID)
	require.NoError(t, e.Advance(ctx, ses.ID))

	final, err := e.View(ctx, ses.ID, alex)
	require.NoError(t, err)
	require.Equal(t, "completed", final.State)
	require.Nil(t, final.Completed.Snapshot, "practice is not scored")
	require.Nil(t, final.Completed.PairID, "practice creates no pair")
	require.Equal(t, "House", *final.Completed.PracticeOpponent)

	u, err := st.GetUser(ctx, alex)
	require.NoError(t, err)
	require.Equal(t, 45, u.XP, "40 seeded + 5 practice completion")
	require.Equal(t, 1, u.Level)
}

func TestStalePollForfeits(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()
	alex := userID(t, st, "Alex")
	jordan := userID(t, st, "Jordan")

	ses, err := e.StartSession(ctx, "this_or_that", "queue_1v1", []string{alex, jordan})
	require.NoError(t, err)
	require.NoError(t, e.Join(ctx, ses.ID, alex))
	require.NoError(t, e.Join(ctx, ses.ID, jordan))

	// Jordan goes quiet for 20s (> the 15s grace).
	_, err = st.DB.Exec(`UPDATE session_participants SET last_poll_at=? WHERE session_id=? AND user_id=?`,
		store.FmtTS(time.Now().UTC().Add(-20*time.Second)), ses.ID, jordan)
	require.NoError(t, err)

	require.NoError(t, e.Advance(ctx, ses.ID))
	got, err := st.GetSession(ctx, ses.ID)
	require.NoError(t, err)
	require.Equal(t, "forfeit", got.State)
	require.NotNil(t, got.EndedAt)
}

func TestPendingSessionCancelsAfter30s(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()
	alex := userID(t, st, "Alex")
	jordan := userID(t, st, "Jordan")

	ses, err := e.StartSession(ctx, "this_or_that", "queue_1v1", []string{alex, jordan})
	require.NoError(t, err)
	_, err = st.DB.Exec(`UPDATE game_sessions SET created_at=? WHERE id=?`,
		store.FmtTS(time.Now().UTC().Add(-31*time.Second)), ses.ID)
	require.NoError(t, err)

	require.NoError(t, e.Advance(ctx, ses.ID))
	got, err := st.GetSession(ctx, ses.ID)
	require.NoError(t, err)
	require.Equal(t, "cancelled", got.State)
	require.NotNil(t, got.EndedAt)
}

func TestGuessMyAnswerRolesAndReveal(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()
	alex := userID(t, st, "Alex")
	jordan := userID(t, st, "Jordan")

	ses, err := e.StartSession(ctx, "guess_my_answer", "queue_1v1", []string{alex, jordan})
	require.NoError(t, err)
	require.NoError(t, e.Join(ctx, ses.ID, alex))
	require.NoError(t, e.Join(ctx, ses.ID, jordan))
	backdate(t, st, ses.ID)
	require.NoError(t, e.Advance(ctx, ses.ID))

	va, err := e.View(ctx, ses.ID, alex)
	require.NoError(t, err)
	require.Equal(t, "in_round", va.State)
	require.Equal(t, 1, va.Round.Index)
	require.Equal(t, 6, va.Round.Total)
	require.NotNil(t, va.Round.YourRole)

	// round 1: seat 0 answers, seat 1 guesses
	require.Equal(t, "guesser", *va.Round.YourRole)
	vj, err := e.View(ctx, ses.ID, jordan)
	require.NoError(t, err)
	require.Equal(t, "answerer", *vj.Round.YourRole)

	// a correct guess scores "same"
	require.NoError(t, e.Answer(ctx, ses.ID, jordan, json.RawMessage(`{"optionId":"b"}`)))
	require.NoError(t, e.Answer(ctx, ses.ID, alex, json.RawMessage(`{"optionId":"b"}`)))
	v, err := e.View(ctx, ses.ID, alex)
	require.NoError(t, err)
	require.Equal(t, "reveal_round", v.State)
	require.True(t, v.Reveal.Same)

	// round 2: roles swap
	backdate(t, st, ses.ID)
	require.NoError(t, e.Advance(ctx, ses.ID))
	va2, err := e.View(ctx, ses.ID, alex)
	require.NoError(t, err)
	require.Equal(t, 2, va2.Round.Index)
	require.Equal(t, "answerer", *va2.Round.YourRole)
	vj2, err := e.View(ctx, ses.ID, jordan)
	require.NoError(t, err)
	require.Equal(t, "guesser", *vj2.Round.YourRole)

	// a wrong guess scores "not same"
	require.NoError(t, e.Answer(ctx, ses.ID, alex, json.RawMessage(`{"optionId":"a"}`)))
	require.NoError(t, e.Answer(ctx, ses.ID, jordan, json.RawMessage(`{"optionId":"c"}`)))
	v2, err := e.View(ctx, ses.ID, alex)
	require.NoError(t, err)
	require.Equal(t, "reveal_round", v2.State)
	require.False(t, v2.Reveal.Same)
}

func TestTwentyQuestionsSameAndDifferent(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()
	alex := userID(t, st, "Alex")
	jordan := userID(t, st, "Jordan")

	ses, err := e.StartSession(ctx, "twenty_questions", "queue_1v1", []string{alex, jordan})
	require.NoError(t, err)
	require.NoError(t, e.Join(ctx, ses.ID, alex))
	require.NoError(t, e.Join(ctx, ses.ID, jordan))
	backdate(t, st, ses.ID)
	require.NoError(t, e.Advance(ctx, ses.ID))

	va, err := e.View(ctx, ses.ID, alex)
	require.NoError(t, err)
	require.Equal(t, "in_round", va.State)
	require.Equal(t, 6, va.Round.Total)
	require.NotNil(t, va.Round.Prompt)
	require.True(t, strings.Contains(string(va.Round.Prompt), `"options"`))
	require.Nil(t, va.Round.YourRole, "20 questions has no answerer/guesser role")

	require.NoError(t, e.Answer(ctx, ses.ID, alex, json.RawMessage(`{"optionId":"a"}`)))
	require.NoError(t, e.Answer(ctx, ses.ID, jordan, json.RawMessage(`{"optionId":"a"}`)))
	v, err := e.View(ctx, ses.ID, alex)
	require.NoError(t, err)
	require.Equal(t, "reveal_round", v.State)
	require.True(t, v.Reveal.Same)

	backdate(t, st, ses.ID)
	require.NoError(t, e.Advance(ctx, ses.ID))
	require.NoError(t, e.Answer(ctx, ses.ID, alex, json.RawMessage(`{"optionId":"a"}`)))
	require.NoError(t, e.Answer(ctx, ses.ID, jordan, json.RawMessage(`{"optionId":"b"}`)))
	v2, err := e.View(ctx, ses.ID, alex)
	require.NoError(t, err)
	require.Equal(t, "reveal_round", v2.State)
	require.False(t, v2.Reveal.Same)
}

func TestLevelForXPBoundaryAtOneHundred(t *testing.T) {
	require.Equal(t, 2, LevelForXP(100))
	require.Equal(t, 2, LevelForXP(299))
	require.Equal(t, 3, LevelForXP(300))
}

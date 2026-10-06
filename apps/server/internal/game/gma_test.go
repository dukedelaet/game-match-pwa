package game

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gamematch/internal/store"
)

// startGMA builds a joined Guess My Answer session sitting in round 1.
func startGMA(t *testing.T, e *Engine, st *store.Store) (string, string, string) {
	t.Helper()
	ctx := context.Background()
	alex := userID(t, st, "Alex")
	jordan := userID(t, st, "Jordan")
	ses, err := e.StartSession(ctx, "guess_my_answer", "queue_1v1", []string{alex, jordan})
	require.NoError(t, err)
	require.NoError(t, e.Join(ctx, ses.ID, alex))
	require.NoError(t, e.Join(ctx, ses.ID, jordan))
	backdate(t, st, ses.ID)
	require.NoError(t, e.Advance(ctx, ses.ID))
	return ses.ID, alex, jordan
}

func TestGuessMyAnswerRunsInTwoPhases(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()
	sessionID, alex, jordan := startGMA(t, e, st)

	round, err := st.RoundByIndex(ctx, sessionID, 1)
	require.NoError(t, err)
	roles, ok := gmaFor(*round)
	require.True(t, ok)
	require.Equal(t, "answer", roles.Phase)

	view, err := e.View(ctx, sessionID, alex)
	require.NoError(t, err)
	require.Equal(t, "in_round", view.State)
	require.NotNil(t, view.Round.Phase)
	require.Equal(t, "answer", *view.Round.Phase)

	// The answerer commits; the round stays open in the guess phase.
	require.NoError(t, e.Answer(ctx, sessionID, jordan, json.RawMessage(`{"optionId":"b"}`)))
	view, err = e.View(ctx, sessionID, alex)
	require.NoError(t, err)
	require.Equal(t, "in_round", view.State)
	require.Equal(t, "guess", *view.Round.Phase)
	require.False(t, view.Round.YouSubmitted, "the guesser has not submitted yet")

	// The guesser matches the secret.
	require.NoError(t, e.Answer(ctx, sessionID, alex, json.RawMessage(`{"optionId":"b"}`)))
	view, err = e.View(ctx, sessionID, alex)
	require.NoError(t, err)
	require.Equal(t, "reveal_round", view.State)
	require.True(t, view.Reveal.Same)

	// Roles swap on the next round.
	backdate(t, st, sessionID)
	require.NoError(t, e.Advance(ctx, sessionID))
	round, err = st.RoundByIndex(ctx, sessionID, 2)
	require.NoError(t, err)
	roles, _ = gmaFor(*round)
	require.Equal(t, alex, roles.Answerer)
	require.Equal(t, jordan, roles.Guesser)
}

func TestGuessMyAnswerRejectsTheWrongRole(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()
	sessionID, alex, jordan := startGMA(t, e, st)

	// The guesser may not submit during the answer phase.
	require.ErrorIs(t, e.Answer(ctx, sessionID, alex, json.RawMessage(`{"optionId":"a"}`)), ErrNotYourTurn)

	// After the answerer commits, the answerer may not submit again.
	require.NoError(t, e.Answer(ctx, sessionID, jordan, json.RawMessage(`{"optionId":"b"}`)))
	require.ErrorIs(t, e.Answer(ctx, sessionID, jordan, json.RawMessage(`{"optionId":"b"}`)), ErrNotYourTurn)
}

func TestGuessMyAnswerAnswerTimeoutSkipsTheRound(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()
	sessionID, alex, _ := startGMA(t, e, st)

	// Nobody answers in time: the round reveals with nothing recorded.
	backdate(t, st, sessionID)
	require.NoError(t, e.Advance(ctx, sessionID))

	view, err := e.View(ctx, sessionID, alex)
	require.NoError(t, err)
	require.Equal(t, "reveal_round", view.State)
	require.False(t, view.Reveal.Same)

	round, err := st.RoundByIndex(ctx, sessionID, 1)
	require.NoError(t, err)
	answers, err := st.AnswersForRound(ctx, round.ID)
	require.NoError(t, err)
	require.Empty(t, answers, "a skipped round records no answers")
}

func TestGuessMyAnswerGuessTimeoutCountsAsIncorrect(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()
	sessionID, alex, jordan := startGMA(t, e, st)

	require.NoError(t, e.Answer(ctx, sessionID, jordan, json.RawMessage(`{"optionId":"b"}`)))

	// The guesser runs out of time.
	backdate(t, st, sessionID)
	require.NoError(t, e.Advance(ctx, sessionID))

	view, err := e.View(ctx, sessionID, alex)
	require.NoError(t, err)
	require.Equal(t, "reveal_round", view.State)
	require.False(t, view.Reveal.Same)
}

func TestTwentyQuestionsUsesATwentySecondTimer(t *testing.T) {
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

	round, err := st.RoundByIndex(ctx, ses.ID, 1)
	require.NoError(t, err)
	require.NotNil(t, round.AnswerBy)

	remaining := store.ParseTS(*round.AnswerBy).Sub(time.Now().UTC())
	require.Greater(t, remaining, 18*time.Second)
	require.LessOrEqual(t, remaining, 20*time.Second)
}

func TestThisOrThatKeepsItsEightSecondTimer(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()
	alex := userID(t, st, "Alex")
	jordan := userID(t, st, "Jordan")

	ses, err := e.StartSession(ctx, "this_or_that", "queue_1v1", []string{alex, jordan})
	require.NoError(t, err)
	require.NoError(t, e.Join(ctx, ses.ID, alex))
	require.NoError(t, e.Join(ctx, ses.ID, jordan))
	backdate(t, st, ses.ID)
	require.NoError(t, e.Advance(ctx, ses.ID))

	round, err := st.RoundByIndex(ctx, ses.ID, 1)
	require.NoError(t, err)
	remaining := store.ParseTS(*round.AnswerBy).Sub(time.Now().UTC())
	require.Greater(t, remaining, 6*time.Second)
	require.LessOrEqual(t, remaining, 8*time.Second)
}

func TestGuessMyAnswerRecordsRoleSpecificSignals(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()
	sessionID, alex, jordan := startGMA(t, e, st)

	for round := 1; round <= 6; round++ {
		row, err := st.RoundByIndex(ctx, sessionID, round)
		require.NoError(t, err)
		require.NotNil(t, row, "round %d", round)
		roles, ok := gmaFor(*row)
		require.True(t, ok)
		require.Equal(t, "answer", roles.Phase)

		require.NoError(t, e.Answer(ctx, sessionID, roles.Answerer, json.RawMessage(`{"optionId":"b"}`)))
		require.NoError(t, e.Answer(ctx, sessionID, roles.Guesser, json.RawMessage(`{"optionId":"b"}`)))

		backdate(t, st, sessionID)
		require.NoError(t, e.Advance(ctx, sessionID))
	}

	got, err := st.GetSession(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, "completed", got.State)
	_ = alex
	_ = jordan

	for _, kind := range []string{"gma_answer", "gma_guess"} {
		var n int
		require.NoError(t, st.DB.Get(&n, `SELECT count(*) FROM signal_events WHERE kind = ?`, kind))
		require.Equal(t, 6, n, "one %s signal per round", kind)
	}

	// The guess payload carries the correctness flag.
	var raw string
	require.NoError(t, st.DB.Get(&raw, `SELECT value FROM signal_events WHERE kind = 'gma_guess' LIMIT 1`))
	var payload struct {
		Correct bool `json:"correct"`
	}
	require.NoError(t, json.Unmarshal([]byte(raw), &payload))
	require.True(t, payload.Correct)
}

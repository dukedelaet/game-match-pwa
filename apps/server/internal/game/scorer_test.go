package game

import (
	"context"
	"encoding/json"
	"testing"

	"github.com/stretchr/testify/require"

	"gamematch/internal/store"
)

func TestJaccardStrictTreatsEmptyUnionAsZero(t *testing.T) {
	require.Equal(t, 0.0, jaccardStrict(nil, nil))
	require.Equal(t, 1.0, jaccardStrict([]string{"a"}, []string{"a"}))
	require.Equal(t, 0.0, jaccardStrict([]string{"a"}, []string{"b"}))
}

func TestBehaviorSimIsUninformedWithoutSharedDimensions(t *testing.T) {
	// Neither user has any observed dimension.
	sim, observed := behaviorSim(vectorFor(store.BehaviorStats{}), vectorFor(store.BehaviorStats{}))
	require.Equal(t, 0.5, sim)
	require.Zero(t, observed)

	// One user has data, the other does not: still uninformed for the pair.
	onlyA := vectorFor(store.BehaviorStats{TempoN: 4, TempoFast: 2})
	sim, observed = behaviorSim(onlyA, vectorFor(store.BehaviorStats{}))
	require.Equal(t, 0.5, sim)
	require.Zero(t, observed)
}

func TestBehaviorSimUsesLaplaceSmoothing(t *testing.T) {
	// Identical histories must be perfectly similar once observed.
	a := vectorFor(store.BehaviorStats{TempoN: 8, TempoFast: 6})
	b := vectorFor(store.BehaviorStats{TempoN: 8, TempoFast: 6})
	sim, observed := behaviorSim(a, b)
	require.Equal(t, 1, observed)
	require.InDelta(t, 1.0, sim, 1e-9)

	// Laplace keeps a zero-history dimension off the extremes.
	require.InDelta(t, 1.0/3.0, laplace(0, 1), 1e-9)
}

// answerRound submits the round's answers in the order the game requires.
func answerRound(t *testing.T, e *Engine, st *store.Store, sessionID string, round int, payloadFor func(userID string) string) {
	t.Helper()
	ctx := context.Background()
	row, err := st.RoundByIndex(ctx, sessionID, round)
	require.NoError(t, err)
	require.NotNil(t, row, "round %d should exist", round)

	var order []string
	if roles, ok := gmaFor(*row); ok {
		order = []string{roles.Answerer, roles.Guesser}
	} else {
		parts, err := st.Participants(ctx, sessionID)
		require.NoError(t, err)
		for _, p := range parts {
			order = append(order, p.UserID)
		}
	}
	for _, uid := range order {
		require.NoError(t, e.Answer(ctx, sessionID, uid, json.RawMessage(payloadFor(uid))))
	}
}

// playFull answers every round and returns the completed session.
func playFull(t *testing.T, e *Engine, st *store.Store, sessionID string, payloadFor func(userID string) string) {
	t.Helper()
	ctx := context.Background()
	ses, err := st.GetSession(ctx, sessionID)
	require.NoError(t, err)
	total := sessionRounds(ses)

	backdate(t, st, sessionID)
	require.NoError(t, e.Advance(ctx, sessionID))

	for round := 1; round <= total; round++ {
		answerRound(t, e, st, sessionID, round, payloadFor)
		backdate(t, st, sessionID)
		require.NoError(t, e.Advance(ctx, sessionID))
	}

	got, err := st.GetSession(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, "completed", got.State)
}

func startJoined(t *testing.T, e *Engine, st *store.Store, kind string) (string, string, string) {
	t.Helper()
	ctx := context.Background()
	alex := userID(t, st, "Alex")
	jordan := userID(t, st, "Jordan")
	ses, err := e.StartSession(ctx, kind, "queue_1v1", []string{alex, jordan})
	require.NoError(t, err)
	require.NoError(t, e.Join(ctx, ses.ID, alex))
	require.NoError(t, e.Join(ctx, ses.ID, jordan))
	return ses.ID, alex, jordan
}

func TestPercentHiddenOnAShortSession(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()
	sessionID, _, _ := startJoined(t, e, st, "twenty_questions")

	// Six answers each is below the eight-answer gate.
	playFull(t, e, st, sessionID, func(string) string { return `{"optionId":"a"}` })

	view, err := e.View(ctx, sessionID, userID(t, st, "Alex"))
	require.NoError(t, err)
	require.NotNil(t, view.Completed)
	require.NotNil(t, view.Completed.Snapshot)
	require.Nil(t, view.Completed.Snapshot.Percent, "percent needs eight tagged answers")
	require.NotEmpty(t, view.Completed.Snapshot.Reasons)
}

func TestPercentShownAfterAFullThisOrThatSession(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()
	sessionID, _, _ := startJoined(t, e, st, "this_or_that")

	playFull(t, e, st, sessionID, func(string) string { return `{"choice":"left"}` })

	view, err := e.View(ctx, sessionID, userID(t, st, "Alex"))
	require.NoError(t, err)
	require.NotNil(t, view.Completed.Snapshot.Percent)
}

func TestSameEnergyReasonAppearsWhenRoundsMatch(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()
	sessionID, _, _ := startJoined(t, e, st, "this_or_that")

	playFull(t, e, st, sessionID, func(string) string { return `{"choice":"left"}` })

	snapshot, err := st.SnapshotForSession(ctx, sessionID)
	require.NoError(t, err)
	require.NotNil(t, snapshot)

	var reasons []string
	require.NoError(t, json.Unmarshal([]byte(*snapshot.Reasons), &reasons))
	require.Contains(t, reasons, "Same energy", "eight matching rounds is same energy")
}

func TestLocationComponentReflectsTheMetro(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()
	jordan := userID(t, st, "Jordan")

	// Put Jordan in another metro: different metros score no location credit.
	var nyID string
	require.NoError(t, st.DB.Get(&nyID, `SELECT id FROM metros WHERE slug = 'new-york'`))
	_, err := st.DB.Exec(`UPDATE users SET metro_id = ? WHERE id = ?`, nyID, jordan)
	require.NoError(t, err)

	sessionID, _, _ := startJoined(t, e, st, "this_or_that")
	playFull(t, e, st, sessionID, func(string) string { return `{"choice":"left"}` })

	snapshot, err := st.SnapshotForSession(ctx, sessionID)
	require.NoError(t, err)
	components := map[string]any{}
	require.NoError(t, json.Unmarshal([]byte(*snapshot.Components), &components))
	require.Equal(t, 0.0, components["location"])
}

func TestBehaviorStatsAreMaterialized(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()
	sessionID, alex, jordan := startJoined(t, e, st, "this_or_that")

	playFull(t, e, st, sessionID, func(string) string { return `{"choice":"left"}` })

	for _, uid := range []string{alex, jordan} {
		stats, err := st.BehaviorStatsFor(ctx, uid)
		require.NoError(t, err)
		require.Equal(t, 8, stats.TempoN, "one tempo observation per answered round")
		require.Equal(t, 1, stats.RematchN, "a completed session is a rematch opportunity")
		require.Zero(t, stats.GuessN, "this_or_that has no guesses")
	}
}

func TestPairCurrentScoreIsWritten(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()
	sessionID, alex, jordan := startJoined(t, e, st, "this_or_that")

	playFull(t, e, st, sessionID, func(string) string { return `{"choice":"left"}` })

	a, b := OrderedPair(alex, jordan)
	var snapshotID string
	require.NoError(t, st.DB.Get(&snapshotID, `
		SELECT snapshot_id FROM pair_current_scores WHERE user_a = ? AND user_b = ?`, a, b))

	snapshot, err := st.SnapshotForSession(ctx, sessionID)
	require.NoError(t, err)
	require.Equal(t, snapshot.ID, snapshotID)
}

func TestGuessCorrectnessFeedsTheGuessDimension(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()
	sessionID, alex, jordan := startJoined(t, e, st, "guess_my_answer")

	// Both players always pick "b", so every guess is correct.
	playFull(t, e, st, sessionID, func(string) string { return `{"optionId":"b"}` })

	for _, uid := range []string{alex, jordan} {
		stats, err := st.BehaviorStatsFor(ctx, uid)
		require.NoError(t, err)
		require.Equal(t, 3, stats.GuessN, "each player guesses in half the rounds")
		require.Equal(t, 3, stats.GuessCorrect)
	}

	snapshot, err := st.SnapshotForSession(ctx, sessionID)
	require.NoError(t, err)
	components := map[string]any{}
	require.NoError(t, json.Unmarshal([]byte(*snapshot.Components), &components))
	require.InDelta(t, 1.0, components["behavior"], 1e-9,
		"identical guess histories give a perfect behaviour similarity")
}

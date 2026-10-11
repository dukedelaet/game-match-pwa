package game

import (
	"context"
	"encoding/json"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"gamematch/internal/store"
)

func TestRegistryHasTwelveReadyGames(t *testing.T) {
	require.Len(t, Registry, 12)
	for _, kind := range RegistryOrder {
		require.True(t, Registry[kind].Ready, "%s must be ready", kind)
	}
}

func TestRegistryInvariants(t *testing.T) {
	for kind, entry := range Registry {
		require.Positive(t, entry.Rounds, "%s rounds", kind)
		require.Positive(t, entry.Timer, "%s timer", kind)
		if kind == "guess_my_answer" {
			require.Empty(t, entry.TagPrefixes, "guesses are not preferences")
			continue
		}
		require.NotEmpty(t, entry.TagPrefixes, "%s tag prefixes", kind)
		require.NotEmpty(t, entry.Chips, "%s chips", kind)
	}
}

func TestRoundsAndTimersComeFromTheRegistry(t *testing.T) {
	require.Equal(t, 3, RoundsFor("either_way"))
	require.Equal(t, 12, RoundsFor("speed_round"))
	require.Equal(t, 5*time.Second, TimerFor("speed_round"))
	require.Equal(t, 15*time.Second, TimerFor("either_way"))
	require.Equal(t, 20*time.Second, TimerFor("twenty_questions"))
}

var newGames = []string{
	"where_do_you_land", "rank_your_top_3", "hot_take", "same_page",
	"speed_round", "one_free_evening", "odd_one_out", "rate_the_night", "either_way",
}

func TestHouseAnswerParsesForEverySeededPrompt(t *testing.T) {
	_, st, _ := newEngineRig(t)
	ctx := context.Background()

	for kind, entry := range Registry {
		var rows []store.Prompt
		require.NoError(t, st.DB.SelectContext(ctx, &rows,
			`SELECT * FROM prompt_bank WHERE game_kind = ? AND active = 1`, kind))
		if entry.Protocol == ProtocolPhased {
			continue // the phased bot path is covered by the existing gma tests
		}
		require.NotEmpty(t, rows, "%s needs prompts", kind)
		for _, prompt := range rows {
			answer := string(HouseAnswerFor(kind, []byte(prompt.Payload)))
			require.NotEmpty(t, answer, "%s house answer", kind)
			assertAnswerShape(t, entry.Protocol, answer)
		}
	}

	// The nine new games ship a full content bank.
	for _, kind := range newGames {
		var count int
		require.NoError(t, st.DB.GetContext(ctx, &count,
			`SELECT count(*) FROM prompt_bank WHERE game_kind = ? AND active = 1`, kind))
		require.GreaterOrEqual(t, count, 12, "%s needs a full content bank", kind)
	}
}

func assertAnswerShape(t *testing.T, protocol Protocol, answer string) {
	t.Helper()
	switch protocol {
	case ProtocolPick2, ProtocolPick4, ProtocolStance, ProtocolCoop, ProtocolRapid:
		var value map[string]any
		require.NoError(t, json.Unmarshal([]byte(answer), &value))
		_, hasOption := value["optionId"]
		_, hasChoice := value["choice"]
		require.True(t, hasOption || hasChoice, "answer %s lacks optionId/choice", answer)
	case ProtocolSpectrum, ProtocolRate:
		var value map[string]any
		require.NoError(t, json.Unmarshal([]byte(answer), &value))
		_, hasStop := value["stop"]
		_, hasRating := value["rating"]
		require.True(t, hasStop || hasRating)
	case ProtocolOrder:
		var value struct {
			Order []string `json:"order"`
		}
		require.NoError(t, json.Unmarshal([]byte(answer), &value))
		require.Len(t, value.Order, 3)
	case ProtocolBranch:
		var value struct {
			Path []string `json:"path"`
		}
		require.NoError(t, json.Unmarshal([]byte(answer), &value))
		require.Len(t, value.Path, 3)
	}
}

func TestRevealsSameComparesEachProtocol(t *testing.T) {
	same := func(protocol Protocol, a, b string) bool {
		return revealsSame(protocol, json.RawMessage(a), json.RawMessage(b))
	}
	// Spectrum and rate treat one step apart as the same.
	require.True(t, same(ProtocolSpectrum, `{"stop":2}`, `{"stop":3}`))
	require.False(t, same(ProtocolSpectrum, `{"stop":2}`, `{"stop":4}`))
	require.True(t, same(ProtocolRate, `{"rating":3}`, `{"rating":4}`))
	require.False(t, same(ProtocolRate, `{"rating":1}`, `{"rating":3}`))
	// Ordering compares the top pick.
	require.True(t, same(ProtocolOrder, `{"order":["a","b","c"]}`, `{"order":["a","c","b"]}`))
	require.False(t, same(ProtocolOrder, `{"order":["a","b","c"]}`, `{"order":["b","a","c"]}`))
	// Branch needs the identical path.
	require.True(t, same(ProtocolBranch, `{"path":["left","left","right"]}`, `{"path":["left","left","right"]}`))
	require.False(t, same(ProtocolBranch, `{"path":["left","left","right"]}`, `{"path":["left","right","left"]}`))
	// Plain picks compare equal.
	require.True(t, same(ProtocolPick4, `{"optionId":"a"}`, `{"optionId":"a"}`))
	require.False(t, same(ProtocolPick4, `{"optionId":"a"}`, `{"optionId":"b"}`))
}

func TestEitherWayPlaysAFullSession(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()
	sessionID, alex, jordan := startJoined(t, e, st, "either_way")

	backdate(t, st, sessionID)
	require.NoError(t, e.Advance(ctx, sessionID))

	path := `{"path":["left","left","right"]}`
	require.NoError(t, e.Answer(ctx, sessionID, alex, json.RawMessage(path)))
	require.NoError(t, e.Answer(ctx, sessionID, jordan, json.RawMessage(path)))

	view, err := e.View(ctx, sessionID, alex)
	require.NoError(t, err)
	require.Equal(t, "reveal_round", view.State)
	require.True(t, view.Reveal.Same)

	// A partial path is rejected without recording anything.
	backdate(t, st, sessionID)
	require.NoError(t, e.Advance(ctx, sessionID))
	require.ErrorIs(t, e.Answer(ctx, sessionID, alex, json.RawMessage(`{"path":["left"]}`)), ErrBadAnswer)
}

func TestSpeedRoundUsesShortRounds(t *testing.T) {
	e, st, _ := newEngineRig(t)
	ctx := context.Background()
	sessionID, alex, _ := startJoined(t, e, st, "speed_round")

	backdate(t, st, sessionID)
	require.NoError(t, e.Advance(ctx, sessionID))

	view, err := e.View(ctx, sessionID, alex)
	require.NoError(t, err)
	require.Equal(t, "in_round", view.State)
	require.Equal(t, 12, view.Round.Total)
}

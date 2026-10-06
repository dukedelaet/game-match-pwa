package game

import (
	"context"
	"encoding/json"
	"math"
	"strings"
	"time"

	"gamematch/internal/store"
)

// behaviorVector is the 4-d vector behind behavior_sim (§Behavior vector).
type behaviorVector struct {
	risk    float64
	tempo   float64
	rematch float64
	guess   float64
	// observed marks the dimensions with data for this user. Dimensions a user
	// has never exercised are excluded rather than padded.
	observed map[string]bool
}

// laplace smooths a counter pair: (sum+1)/(n+2).
func laplace(sum, n int) float64 { return float64(sum+1) / float64(n+2) }

func vectorFor(stats store.BehaviorStats) behaviorVector {
	v := behaviorVector{observed: map[string]bool{}}
	if stats.RiskN > 0 {
		v.risk = laplace(stats.RiskSum, stats.RiskN)
		v.observed["risk"] = true
	}
	if stats.TempoN > 0 {
		v.tempo = laplace(stats.TempoFast, stats.TempoN)
		v.observed["tempo"] = true
	}
	if stats.RematchN > 0 {
		v.rematch = laplace(stats.RematchYes, stats.RematchN)
		v.observed["rematch"] = true
	}
	if stats.GuessN > 0 {
		v.guess = laplace(stats.GuessCorrect, stats.GuessN)
		v.observed["guess"] = true
	}
	return v
}

// behaviorSim is the cosine similarity over the dimensions both users have
// observed. With no shared dimension it returns the uninformed 0.5.
func behaviorSim(a, b behaviorVector) (float64, int) {
	dims := []string{"risk", "tempo", "rematch", "guess"}
	av := []float64{a.risk, a.tempo, a.rematch, a.guess}
	bv := []float64{b.risk, b.tempo, b.rematch, b.guess}

	var dot, normA, normB float64
	shared := 0
	for i, dim := range dims {
		if !a.observed[dim] || !b.observed[dim] {
			continue
		}
		shared++
		dot += av[i] * bv[i]
		normA += av[i] * av[i]
		normB += bv[i] * bv[i]
	}
	if shared == 0 {
		return 0.5, 0
	}
	if normA == 0 || normB == 0 {
		return 0, shared
	}
	return dot / (math.Sqrt(normA) * math.Sqrt(normB)), shared
}

// recordBehavior materializes user_behavior_stats from a finished session.
// Practice sessions are never counted.
func (e *Engine) recordBehavior(ctx context.Context, ses store.GameSession) error {
	if ses.Mode == modePractice {
		return nil
	}
	parts, err := e.Store.Participants(ctx, ses.ID)
	if err != nil {
		return err
	}
	rounds, err := e.Store.RoundsFor(ctx, ses.ID)
	if err != nil {
		return err
	}

	for _, participant := range parts {
		delta := store.BehaviorStats{UserID: participant.UserID, RematchN: 1}
		for _, round := range rounds {
			answers, err := e.Store.AnswersForRound(ctx, round.ID)
			if err != nil {
				return err
			}
			mine := answerFor(answers, participant.UserID)
			if mine == nil {
				continue // timeouts are not behaviour
			}
			for _, tag := range e.optionTags(ctx, round, mine.Payload) {
				if strings.HasPrefix(tag, "behavior.risk") {
					delta.RiskN++
					if strings.HasSuffix(tag, "high") {
						delta.RiskSum++
					}
				}
			}
			delta.TempoN++
			if fastSubmit(ses.Kind, round, mine) {
				delta.TempoFast++
			}
			if roles, ok := gmaFor(round); ok && participant.UserID == roles.Guesser {
				delta.GuessN++
				if secret := answerFor(answers, roles.Answerer); secret != nil && guessIsCorrect(secret.Payload, mine.Payload) {
					delta.GuessCorrect++
				}
			}
		}
		if err := e.Store.AddBehaviorStats(ctx, delta); err != nil {
			return err
		}
	}
	return nil
}

// roundWindow is how long a round's owner had to answer.
func roundWindow(kind string, round store.Round) time.Duration {
	if roles, ok := gmaFor(round); ok && roles.Phase == "guess" {
		return guessWindow
	}
	switch kind {
	case "this_or_that":
		return 8 * time.Second
	case "twenty_questions":
		return 20 * time.Second
	case "guess_my_answer":
		return 8 * time.Second
	}
	return 8 * time.Second
}

// fastSubmit reports whether an answer landed in the first half of its window.
func fastSubmit(kind string, round store.Round, answer *store.RoundAnswer) bool {
	if round.AnswerBy == nil || answer.SubmittedAt == nil {
		return false
	}
	window := roundWindow(kind, round)
	deadline := store.ParseTS(*round.AnswerBy)
	start := deadline.Add(-window)
	submitted := store.ParseTS(*answer.SubmittedAt)
	return !submitted.After(start.Add(window / 2))
}

// optionTags returns the tags of the option a player picked in a round.
func (e *Engine) optionTags(ctx context.Context, round store.Round, answer string) []string {
	if round.PromptID == nil {
		return nil
	}
	prompt, err := e.Store.PromptByID(ctx, *round.PromptID)
	if err != nil || prompt == nil {
		return nil
	}
	return tagsFromPayload(prompt.Payload, answer)
}

// pickedTags collects the option tags a user chose in this_or_that and
// twenty_questions rounds (Guess My Answer guesses are not preferences).
func (e *Engine) pickedTags(ctx context.Context, userID string) ([]string, error) {
	rows := []struct {
		Kind    string `db:"kind"`
		Payload string `db:"payload"`
		Answer  string `db:"answer"`
	}{}
	err := e.Store.DB.SelectContext(ctx, &rows, `
		SELECT gs.kind AS kind, p.payload AS payload, ra.payload AS answer
		FROM round_answers ra
		JOIN rounds r ON r.id = ra.round_id
		JOIN game_sessions gs ON gs.id = r.session_id
		JOIN prompt_bank p ON p.id = r.prompt_id
		WHERE ra.user_id = ? AND gs.mode != 'practice'
		  AND gs.kind IN ('this_or_that','twenty_questions')`, userID)
	if err != nil {
		return nil, err
	}
	var tags []string
	for _, row := range rows {
		tags = append(tags, tagsFromPayload(row.Payload, row.Answer)...)
	}
	return tags, nil
}

// tagsFromPayload pulls the selected option's tags straight from prompt JSON.
func tagsFromPayload(promptPayload, answer string) []string {
	var payload struct {
		Left struct {
			ID   string   `json:"id"`
			Tags []string `json:"tags"`
		} `json:"left"`
		Right struct {
			ID   string   `json:"id"`
			Tags []string `json:"tags"`
		} `json:"right"`
		Options []struct {
			ID   string   `json:"id"`
			Tags []string `json:"tags"`
		} `json:"options"`
	}
	if err := json.Unmarshal([]byte(promptPayload), &payload); err != nil {
		return nil
	}
	var choice string
	var decoded map[string]any
	if json.Unmarshal([]byte(answer), &decoded) == nil {
		if v, ok := decoded["choice"].(string); ok {
			choice = v
		}
		if v, ok := decoded["optionId"].(string); ok {
			choice = v
		}
	}
	switch choice {
	case "left":
		return payload.Left.Tags
	case "right":
		return payload.Right.Tags
	}
	for _, option := range payload.Options {
		if option.ID == choice {
			return option.Tags
		}
	}
	return nil
}

// jaccardStrict is the Jaccard index with empty∪empty defined as 0.
func jaccardStrict(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0
	}
	setA := map[string]bool{}
	for _, x := range a {
		setA[x] = true
	}
	inter := 0
	union := map[string]bool{}
	for _, x := range a {
		union[x] = true
	}
	for _, x := range b {
		union[x] = true
		if setA[x] {
			inter++
		}
	}
	if len(union) == 0 {
		return 0
	}
	return float64(inter) / float64(len(union))
}

// keysWithPrefix filters tag-like strings to those starting with prefix.
func keysWithPrefix(tags []string, prefix string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, tag := range tags {
		if !strings.HasPrefix(tag, prefix) || seen[tag] {
			continue
		}
		seen[tag] = true
		out = append(out, tag)
	}
	return out
}

// union merges string slices without duplicates.
func union(groups ...[]string) []string {
	out := []string{}
	seen := map[string]bool{}
	for _, group := range groups {
		for _, item := range group {
			if item == "" || seen[item] {
				continue
			}
			seen[item] = true
			out = append(out, item)
		}
	}
	return out
}

// topSharedInterest returns the shared interest.* tag with the most support, or
// "" when the two users share no interest tag.
func topSharedInterest(a, b []string) string {
	setB := map[string]bool{}
	for _, x := range b {
		setB[x] = true
	}
	counts := map[string]int{}
	for _, x := range a {
		if setB[x] && strings.HasPrefix(x, "interest.") {
			counts[x]++
		}
	}
	best, bestCount := "", 0
	for tag, n := range counts {
		if n > bestCount || (n == bestCount && tag < best) {
			best, bestCount = tag, n
		}
	}
	return best
}

// adjacentMetro reports whether two metros are listed as adjacent.
func (e *Engine) adjacentMetro(ctx context.Context, a, b string) bool {
	metro, err := e.Store.MetroByID(ctx, a)
	if err != nil || metro == nil || metro.AdjacentIDs == nil {
		return false
	}
	var adjacent []string
	if err := json.Unmarshal([]byte(*metro.AdjacentIDs), &adjacent); err != nil {
		return false
	}
	for _, id := range adjacent {
		if id == b {
			return true
		}
	}
	return false
}

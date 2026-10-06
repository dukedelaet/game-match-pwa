package game

import (
	"context"
	"encoding/json"
	"reflect"

	"gamematch/internal/store"
)

// Score is the outcome of scorer_v0 for a completed session.
type Score struct {
	Score      float64
	Percent    *int
	Reasons    []string
	Components map[string]any
}

// ScoreSession ports Scorer::scoreSession. It returns nil for practice
// sessions or sessions without exactly two participants.
func (e *Engine) ScoreSession(ctx context.Context, sessionID string) (*Score, error) {
	ses, err := e.Store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if ses.Mode == "practice" {
		return nil, nil
	}
	parts, err := e.Store.Participants(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if len(parts) != 2 {
		return nil, nil
	}
	a, b := OrderedPair(parts[0].UserID, parts[1].UserID)

	ua, err := e.Store.GetUser(ctx, a)
	if err != nil {
		return nil, err
	}
	ub, err := e.Store.GetUser(ctx, b)
	if err != nil {
		return nil, err
	}
	ta, _ := e.Store.TraitIDsFor(ctx, a)
	tb, _ := e.Store.TraitIDsFor(ctx, b)
	jTraits := jaccard(ta, tb)

	ia, _ := e.Store.IntentsFor(ctx, a)
	ib, _ := e.Store.IntentsFor(ctx, b)
	jInt := jaccard(ia, ib)

	rounds, err := e.Store.RoundsFor(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	same, n := 0, 0
	type roundAnswers struct {
		round   store.Round
		answers []store.RoundAnswer
	}
	var perRound []roundAnswers
	for _, r := range rounds {
		ans, err := e.Store.AnswersForRound(ctx, r.ID)
		if err != nil {
			return nil, err
		}
		perRound = append(perRound, roundAnswers{round: r, answers: ans})
		if len(ans) < 2 {
			continue
		}
		n++
		if jsonEqual(ans[0].Payload, ans[1].Payload) {
			same++
		}
	}
	behavior := 0.5
	if n > 0 {
		behavior = float64(same) / float64(n)
	}
	loc := 0.0
	if ua.MetroID != nil && ub.MetroID != nil && *ua.MetroID == *ub.MetroID {
		loc = 0.6
	}

	const (
		wP = 0.3125
		wI = 0.25
		wL = 0.1875
		wB = 0.1875
		wG = 0.0625
	)
	score := wP*jTraits + wI*jInt + wL*jTraits + wB*behavior + wG*loc
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	reasons := []string{}
	if jTraits >= 0.3 {
		reasons = append(reasons, "Similar vibe")
	}
	if behavior >= 0.5 {
		reasons = append(reasons, "Same game energy")
	}
	if jInt >= 0.5 {
		reasons = append(reasons, "Same reasons for being here")
	}
	if loc > 0 {
		reasons = append(reasons, "Same city")
	}
	if len(reasons) == 0 {
		reasons = []string{"Still getting to know you"}
	}
	if len(reasons) > 3 {
		reasons = reasons[:3]
	}

	var percent *int
	if n >= 6 {
		p := int(score*100 + 0.5)
		percent = &p
	}

	for _, pr := range perRound {
		for _, uid := range []string{a, b} {
			for _, ans := range pr.answers {
				if ans.UserID != uid {
					continue
				}
				sid := sessionID
				val := ans.Payload
				if err := e.Store.InsertSignalEvent(ctx, uid, &sid, "choice", "round."+itoa(pr.round.RoundIndex), &val); err != nil {
					return nil, err
				}
			}
		}
	}

	components := map[string]any{
		"personality": jTraits,
		"interests":   jInt,
		"lifestyle":   jTraits,
		"behavior":    behavior,
		"location":    loc,
		"percent":     percent,
	}
	compJSON, err := json.Marshal(components)
	if err != nil {
		return nil, err
	}
	reasonsJSON, err := json.Marshal(reasons)
	if err != nil {
		return nil, err
	}
	sessionIDCopy := sessionID
	snap := &store.Snapshot{
		SessionID:  sessionIDCopy,
		UserA:      a,
		UserB:      b,
		Score:      score,
		Components: ptr(string(compJSON)),
		Reasons:    ptr(string(reasonsJSON)),
	}
	if err := e.Store.InsertSnapshot(ctx, snap); err != nil {
		return nil, err
	}

	return &Score{Score: score, Percent: percent, Reasons: reasons, Components: components}, nil
}

func jaccard(a, b []string) float64 {
	if len(a) == 0 && len(b) == 0 {
		return 0.5
	}
	set := map[string]bool{}
	for _, x := range a {
		set[x] = true
	}
	inter := 0
	for _, x := range b {
		if set[x] {
			inter++
		}
	}
	unionSet := map[string]bool{}
	for _, x := range a {
		unionSet[x] = true
	}
	for _, x := range b {
		unionSet[x] = true
	}
	if len(unionSet) == 0 {
		return 0
	}
	return float64(inter) / float64(len(unionSet))
}

// jsonEqual compares two JSON documents by value (order-insensitive).
func jsonEqual(a, b string) bool {
	var av, bv any
	if err := json.Unmarshal([]byte(a), &av); err != nil {
		return a == b
	}
	if err := json.Unmarshal([]byte(b), &bv); err != nil {
		return a == b
	}
	return reflect.DeepEqual(av, bv)
}

func ptr[T any](v T) *T { return &v }

func itoa(i int) string {
	if i == 0 {
		return "0"
	}
	neg := i < 0
	if neg {
		i = -i
	}
	var buf [20]byte
	pos := len(buf)
	for i > 0 {
		pos--
		buf[pos] = byte('0' + i%10)
		i /= 10
	}
	if neg {
		pos--
		buf[pos] = '-'
	}
	return string(buf[pos:])
}

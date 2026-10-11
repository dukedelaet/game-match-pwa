package game

import (
	"context"
	"encoding/json"
	"reflect"
	"sort"
	"strings"

	"gamematch/internal/store"
)

// Score is the outcome of scorer_v0 for a completed session.
type Score struct {
	Score      float64
	Percent    *int
	Reasons    []string
	Components map[string]any
}

// chip is a candidate reason with its ranking weight.
type chip struct {
	label string
	rank  float64
}

// ScoreSession ports scorer_v0. It returns nil for practice sessions and for
// sessions without exactly two participants.
func (e *Engine) ScoreSession(ctx context.Context, sessionID string) (*Score, error) {
	ses, err := e.Store.GetSession(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	if ses.Mode == modePractice {
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

	axesA, err := e.Store.TraitSlugsByAxis(ctx, a)
	if err != nil {
		return nil, err
	}
	axesB, err := e.Store.TraitSlugsByAxis(ctx, b)
	if err != nil {
		return nil, err
	}
	tagsA, err := e.pickedTags(ctx, a)
	if err != nil {
		return nil, err
	}
	tagsB, err := e.pickedTags(ctx, b)
	if err != nil {
		return nil, err
	}
	gamesA, err := e.Store.FavoriteGames(ctx, a)
	if err != nil {
		return nil, err
	}
	gamesB, err := e.Store.FavoriteGames(ctx, b)
	if err != nil {
		return nil, err
	}

	// P = personality traits; L = lifestyle traits + lifestyle.* picks;
	// I = interest traits + favorite games + interest.* picks; the four new
	// axes are tag-derived only.
	setP := func(axes map[string][]string) []string { return axes["personality"] }
	jP := jaccardStrict(setP(axesA), setP(axesB))
	lA := union(axesA["lifestyle"], keysWithPrefix(tagsA, "lifestyle."))
	lB := union(axesB["lifestyle"], keysWithPrefix(tagsB, "lifestyle."))
	jL := jaccardStrict(lA, lB)
	iA := union(axesA["interest"], gamesA, keysWithPrefix(tagsA, "interest."))
	iB := union(axesB["interest"], gamesB, keysWithPrefix(tagsB, "interest."))
	jI := jaccardStrict(iA, iB)
	jV := jaccardStrict(keysWithPrefix(tagsA, "values."), keysWithPrefix(tagsB, "values."))
	jPr := jaccardStrict(keysWithPrefix(tagsA, "priorities."), keysWithPrefix(tagsB, "priorities."))
	jM := jaccardStrict(keysWithPrefix(tagsA, "mindset."), keysWithPrefix(tagsB, "mindset."))
	jE := jaccardStrict(keysWithPrefix(tagsA, "energy."), keysWithPrefix(tagsB, "energy."))

	statsA, err := e.Store.BehaviorStatsFor(ctx, a)
	if err != nil {
		return nil, err
	}
	statsB, err := e.Store.BehaviorStatsFor(ctx, b)
	if err != nil {
		return nil, err
	}
	bSim, observedDims := behaviorSim(vectorFor(statsA), vectorFor(statsB))

	location := 0.0
	if ua.MetroID != nil && ub.MetroID != nil {
		switch {
		case *ua.MetroID == *ub.MetroID:
			location = 1.0
		case e.adjacentMetro(ctx, *ua.MetroID, *ub.MetroID):
			location = 0.5
		}
	}

	const (
		wP  = 0.15
		wI  = 0.15
		wV  = 0.15
		wB  = 0.15
		wL  = 0.10
		wPr = 0.10
		wM  = 0.10
		wE  = 0.05
		wG  = 0.05
	)
	score := wP*jP + wI*jI + wV*jV + wB*bSim + wL*jL + wPr*jPr + wM*jM + wE*jE + wG*location
	if score < 0 {
		score = 0
	}
	if score > 1 {
		score = 1
	}

	// Walk the session once for signals, the same-energy count, and GMA accuracy.
	rounds, err := e.Store.RoundsFor(ctx, sessionID)
	if err != nil {
		return nil, err
	}
	type roundAnswers struct {
		round   store.Round
		answers []store.RoundAnswer
	}
	perRound := make([]roundAnswers, 0, len(rounds))
	sameRounds := 0
	gmaGuesses, gmaCorrect := 0, 0
	for _, round := range rounds {
		answers, err := e.Store.AnswersForRound(ctx, round.ID)
		if err != nil {
			return nil, err
		}
		perRound = append(perRound, roundAnswers{round: round, answers: answers})
		if len(answers) < 2 {
			continue
		}
		if revealsSame(For(ses.Kind).Protocol, json.RawMessage(answers[0].Payload), json.RawMessage(answers[1].Payload)) {
			sameRounds++
		}
		if roles, ok := gmaFor(round); ok {
			secret := answerFor(answers, roles.Answerer)
			guess := answerFor(answers, roles.Guesser)
			if secret != nil && guess != nil {
				gmaGuesses++
				if guessIsCorrect(secret.Payload, guess.Payload) {
					gmaCorrect++
				}
			}
		}
	}

	// Reasons, ranked by the table in §Reason copy.
	chips := []chip{}
	if jL >= 0.5 {
		chips = append(chips, chip{"Similar lifestyle", wL * jL})
	}
	if bSim >= 0.7 && observedDims > 0 {
		chips = append(chips, chip{"Similar game style", wB * bSim})
	}
	if jP >= 0.3 && contains(setP(axesA), "funny") && contains(setP(axesB), "funny") {
		chips = append(chips, chip{"Same humor", wP * jP})
	}
	if gmaGuesses > 0 {
		accuracy := float64(gmaCorrect) / float64(gmaGuesses)
		if accuracy >= 0.5 {
			chips = append(chips, chip{"Reads the room", wB * accuracy})
		}
	}
	if jI >= 0.3 {
		switch topShared := topSharedInterest(iA, iB); {
		case strings.HasPrefix(topShared, "interest.music"):
			chips = append(chips, chip{"Similar music", wI * jI})
		case strings.HasPrefix(topShared, "interest.food"):
			chips = append(chips, chip{"Similar food", wI * jI})
		default:
			chips = append(chips, chip{"Similar interests", wI * jI})
		}
	}
	// The four tag-derived axes reuse this session's game chip when it owns the
	// axis, so the copy stays specific ("Closer than you'd think", not "values
	// overlap").
	sessionGame := For(ses.Kind)
	axisChip := func(defaultChip, prefix string) string {
		for _, owned := range sessionGame.TagPrefixes {
			if owned == prefix && len(sessionGame.Chips) > 0 {
				return sessionGame.Chips[0]
			}
		}
		return defaultChip
	}
	if jV >= 0.5 {
		chips = append(chips, chip{axisChip("Agree where it counts", "values."), wV * jV})
	}
	if jPr >= 0.5 {
		chips = append(chips, chip{axisChip("Same number one", "priorities."), wPr * jPr})
	}
	if jM >= 0.5 {
		chips = append(chips, chip{axisChip("On the same page", "mindset."), wM * jM})
	}
	if jE >= 0.5 {
		chips = append(chips, chip{axisChip("Same speed", "energy."), wE * jE})
	}
	sort.SliceStable(chips, func(i, j int) bool { return chips[i].rank > chips[j].rank })

	reasons := []string{}
	for i := 0; i < len(chips) && i < 3; i++ {
		reasons = append(reasons, chips[i].label)
	}
	if sameRounds >= 3 {
		// Same energy is a bonus: it slides in below the strongest chip, and
		// otherwise takes the weakest slot.
		if len(reasons) < 3 {
			reasons = append(reasons, "Same energy")
		} else {
			reasons[len(reasons)-1] = "Same energy"
		}
	}
	if len(reasons) == 0 {
		reasons = []string{"Still getting to know you"}
	}

	// A percent needs enough signal on both sides.
	taggedA, err := e.Store.TaggedAnswerCount(ctx, a)
	if err != nil {
		return nil, err
	}
	taggedB, err := e.Store.TaggedAnswerCount(ctx, b)
	if err != nil {
		return nil, err
	}
	var percent *int
	if taggedA >= 8 && taggedB >= 8 {
		p := int(score*100 + 0.5)
		percent = &p
	}

	// Behaviour signals, one per answered round.
	for _, entry := range perRound {
		roles, isGMA := gmaFor(entry.round)
		for _, uid := range []string{a, b} {
			for _, answer := range entry.answers {
				if answer.UserID != uid {
					continue
				}
				identifier := sessionID
				value := answer.Payload
				kind := "choice"
				if isGMA {
					switch uid {
					case roles.Answerer:
						kind = "gma_answer"
					case roles.Guesser:
						kind = "gma_guess"
						correct := false
						if secret := answerFor(entry.answers, roles.Answerer); secret != nil {
							correct = guessIsCorrect(secret.Payload, answer.Payload)
						}
						encoded, _ := json.Marshal(map[string]bool{"correct": correct})
						value = string(encoded)
					}
				}
				if err := e.Store.InsertSignalEvent(ctx, uid, &identifier, kind, "round."+itoa(entry.round.RoundIndex), &value); err != nil {
					return nil, err
				}
			}
		}
	}

	components := map[string]any{
		"personality": jP,
		"interests":   jI,
		"lifestyle":   jL,
		"values":      jV,
		"priorities":  jPr,
		"mindset":     jM,
		"energy":      jE,
		"behavior":    bSim,
		"location":    location,
		"percent":     percent,
	}
	componentsJSON, err := json.Marshal(components)
	if err != nil {
		return nil, err
	}
	reasonsJSON, err := json.Marshal(reasons)
	if err != nil {
		return nil, err
	}
	snapshot := &store.Snapshot{
		SessionID:  sessionID,
		UserA:      a,
		UserB:      b,
		Score:      score,
		Components: ptr(string(componentsJSON)),
		Reasons:    ptr(string(reasonsJSON)),
	}
	if err := e.Store.InsertSnapshot(ctx, snapshot); err != nil {
		return nil, err
	}
	if err := e.Store.UpsertPairScore(ctx, a, b, snapshot.ID); err != nil {
		return nil, err
	}

	return &Score{Score: score, Percent: percent, Reasons: reasons, Components: components}, nil
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

// guessIsCorrect reports whether a Guess My Answer guess matched the secret.
func guessIsCorrect(secret, guess string) bool {
	var s, g struct {
		OptionID string `json:"optionId"`
	}
	if json.Unmarshal([]byte(secret), &s) != nil || json.Unmarshal([]byte(guess), &g) != nil {
		return false
	}
	return s.OptionID != "" && s.OptionID == g.OptionID
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

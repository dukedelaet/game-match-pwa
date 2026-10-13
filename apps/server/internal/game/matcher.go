package game

import (
	"context"
	"encoding/json"
	"sync"
	"time"

	"gamematch/internal/config"
	"gamematch/internal/store"
)

// Matcher pairs queued players with hard filters.
type Matcher struct {
	Store *store.Store
	Cfg   config.Config
	mu    sync.Mutex
}

// NewMatcher builds a matcher.
func NewMatcher(s *store.Store, cfg config.Config) *Matcher {
	return &Matcher{Store: s, Cfg: cfg}
}

// QueueStatus mirrors the shapes returned by GET /queue/status.
type QueueStatus struct {
	State            string `json:"state"`
	SessionID        string `json:"sessionId,omitempty"`
	GameKind         string `json:"gameKind,omitempty"`
	PositionHint     string `json:"positionHint,omitempty"`
	EstimatedWaitSec int    `json:"estimatedWaitSec,omitempty"`
	WaitedSec        int    `json:"waitedSec,omitempty"`
	CanPractice      bool   `json:"canPractice,omitempty"`
}

// Enqueue replaces any existing queue row for the user.
func (m *Matcher) Enqueue(ctx context.Context, userID, kind string, allowPractice bool) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := m.Store.QueueDeleteForUser(ctx, userID); err != nil {
		return err
	}
	u, err := m.Store.GetUser(ctx, userID)
	if err != nil {
		return err
	}
	return m.Store.QueueInsert(ctx, &store.QueueEntry{
		UserID:        userID,
		MetroID:       u.MetroID,
		GameKind:      kind,
		AllowPractice: allowPractice,
	})
}

// Leave removes an unmatched queue row.
func (m *Matcher) Leave(ctx context.Context, userID string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, err := m.Store.QueueEntryForUser(ctx, userID)
	if err != nil || entry == nil {
		return err
	}
	if entry.MatchedSessionID != nil {
		return nil
	}
	return m.Store.QueueDeleteForUser(ctx, userID)
}

// Tick reports queue state, attempting a match.
func (m *Matcher) Tick(ctx context.Context, userID string) (QueueStatus, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	entry, err := m.Store.QueueEntryForUser(ctx, userID)
	if err != nil {
		return QueueStatus{}, err
	}
	if entry == nil {
		return QueueStatus{State: "idle"}, nil
	}
	if entry.MatchedSessionID != nil {
		return QueueStatus{State: "matched", SessionID: *entry.MatchedSessionID, GameKind: entry.GameKind}, nil
	}

	waited := int(time.Since(store.ParseTS(entry.EnqueuedAt)).Seconds())
	if waited < 0 {
		waited = 0
	}

	session, err := m.tryMatch(ctx, entry)
	if err != nil {
		return QueueStatus{}, err
	}
	if session != nil {
		return QueueStatus{State: "matched", SessionID: session.ID, GameKind: entry.GameKind}, nil
	}

	if entry.AllowPractice && waited >= int(practiceAfter.Seconds()) && m.Cfg.HouseUserID != "" {
		practice, err := m.startSession(ctx, entry.GameKind, modePractice, []string{userID, m.Cfg.HouseUserID})
		if err != nil {
			return QueueStatus{}, err
		}
		if err := m.Store.QueueMarkMatched(ctx, entry.ID, practice.ID); err != nil {
			return QueueStatus{}, err
		}
		return QueueStatus{State: "matched", SessionID: practice.ID, GameKind: entry.GameKind}, nil
	}

	return QueueStatus{
		State:            "waiting",
		GameKind:         entry.GameKind,
		PositionHint:     "searching",
		EstimatedWaitSec: 45,
		WaitedSec:        waited,
		CanPractice:      waited >= int(practiceAfter.Seconds()),
	}, nil
}

type candidate struct {
	entry store.QueueEntry
	user  store.User
}

func (m *Matcher) tryMatch(ctx context.Context, entry *store.QueueEntry) (*store.GameSession, error) {
	me, err := m.Store.GetUser(ctx, entry.UserID)
	if err != nil {
		return nil, nil
	}
	if me.Incognito || me.Hidden || me.Status != "active" {
		return nil, nil
	}

	metroIDs := []string{}
	if entry.MetroID != nil {
		metroIDs = append(metroIDs, *entry.MetroID)
	}
	waited := int(time.Since(store.ParseTS(entry.EnqueuedAt)).Seconds())
	if waited < 0 {
		waited = 0
	}
	if metro, _ := m.Store.MetroByID(ctx, deref(entry.MetroID)); metro != nil && waited >= int(metroExpand.Seconds()) && metro.AdjacentIDs != nil {
		var adjacent []string
		if err := json.Unmarshal([]byte(*metro.AdjacentIDs), &adjacent); err == nil {
			metroIDs = append(metroIDs, adjacent...)
		}
	}

	entries, err := m.Store.QueueCandidates(ctx, entry.GameKind, entry.UserID, metroIDs, 20)
	if err != nil {
		return nil, err
	}

	// Load everything the eligibility rules need in a fixed number of queries.
	// Doing this per candidate made one matching pass cost roughly 15 x 20
	// statements, on the endpoint the PWA polls.
	ids := make([]string, 0, len(entries))
	for _, e := range entries {
		ids = append(ids, e.UserID)
	}
	users, err := m.Store.UsersByIDs(ctx, ids)
	if err != nil {
		return nil, err
	}
	blocked, err := m.Store.BlockedPairs(ctx, me.ID, ids)
	if err != nil {
		return nil, err
	}
	all := append([]string{me.ID}, ids...)
	profiles, err := m.Store.ProfilesByIDs(ctx, all)
	if err != nil {
		return nil, err
	}
	prefs, err := m.Store.PreferencesByIDs(ctx, all)
	if err != nil {
		return nil, err
	}
	intents, err := m.Store.IntentsByIDs(ctx, all)
	if err != nil {
		return nil, err
	}
	el := &eligibility{
		me:       eligibilityRow{intents: intents[me.ID], profile: profiles[me.ID], pref: prefs[me.ID]},
		profiles: profiles,
		prefs:    prefs,
		intents:  intents,
	}

	var best []candidate
	for _, other := range entries {
		them, ok := users[other.UserID]
		if !ok {
			continue
		}
		if them.Incognito || them.Hidden || them.Status != "active" {
			continue
		}
		if blocked[them.ID] {
			continue
		}
		if !el.ok(them.ID) {
			continue
		}
		best = append(best, candidate{entry: other, user: them})
		if len(best) >= 5 {
			break
		}
	}
	if len(best) == 0 {
		return nil, nil
	}
	pick := best[0]
	session, err := m.startSession(ctx, entry.GameKind, "queue_1v1", []string{entry.UserID, pick.entry.UserID})
	if err != nil {
		return nil, err
	}
	if err := m.Store.QueueMarkMatched(ctx, entry.ID, session.ID); err != nil {
		return nil, err
	}
	if err := m.Store.QueueMarkMatched(ctx, pick.entry.ID, session.ID); err != nil {
		return nil, err
	}
	return &session, nil
}

// mutuallyEligible is kept for callers outside the batched matching path.
func (m *Matcher) mutuallyEligible(ctx context.Context, me, them store.User) bool {
	ids := []string{me.ID, them.ID}
	profiles, err := m.Store.ProfilesByIDs(ctx, ids)
	if err != nil {
		return false
	}
	prefs, err := m.Store.PreferencesByIDs(ctx, ids)
	if err != nil {
		return false
	}
	intents, err := m.Store.IntentsByIDs(ctx, ids)
	if err != nil {
		return false
	}
	el := &eligibility{
		me:       eligibilityRow{intents: intents[me.ID], profile: profiles[me.ID], pref: prefs[me.ID]},
		profiles: profiles,
		prefs:    prefs,
		intents:  intents,
	}
	return el.ok(them.ID)
}

func (m *Matcher) startSession(ctx context.Context, kind, mode string, userIDs []string) (store.GameSession, error) {
	engine := NewEngine(m.Store, m.Cfg)
	return engine.StartSession(ctx, kind, mode, userIDs)
}

func deref(s *string) string {
	if s == nil {
		return ""
	}
	return *s
}

// eligibilityRow and eligibility hold the per-user data the matching rules need,
// loaded in bulk so the query count does not grow with the candidate count.
type eligibilityRow struct {
	intents []string
	profile *store.Profile
	pref    *store.Preference
}

type eligibility struct {
	me       eligibilityRow
	profiles map[string]*store.Profile
	prefs    map[string]*store.Preference
	intents  map[string][]string
}

// ok applies the matching rules for both directions.
func (e *eligibility) ok(otherID string) bool {
	them := eligibilityRow{
		intents: e.intents[otherID],
		profile: e.profiles[otherID],
		pref:    e.prefs[otherID],
	}
	if !IntentsOK(e.me.intents, them.intents) {
		return false
	}

	var meAge, themAge *int
	var meGender, themGender *string
	if e.me.profile != nil {
		meAge, meGender = e.me.profile.Age, e.me.profile.GenderID
	}
	if them.profile != nil {
		themAge, themGender = them.profile.Age, them.profile.GenderID
	}
	if !AgeOK(meAge, e.me.pref, themAge, them.pref) {
		return false
	}
	if !GenderOK(e.me.pref, themGender) || !GenderOK(them.pref, meGender) {
		return false
	}
	return true
}

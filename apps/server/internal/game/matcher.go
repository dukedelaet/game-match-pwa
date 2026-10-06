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

	var best []candidate
	for _, other := range entries {
		them, err := m.Store.GetUser(ctx, other.UserID)
		if err != nil {
			continue
		}
		if them.Incognito || them.Hidden || them.Status != "active" {
			continue
		}
		blocked, err := m.Store.BlockedEitherWay(ctx, me.ID, them.ID)
		if err != nil {
			return nil, err
		}
		if blocked {
			continue
		}
		if !m.mutuallyEligible(ctx, me, them) {
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

func (m *Matcher) mutuallyEligible(ctx context.Context, me, them store.User) bool {
	meIntents, _ := m.Store.IntentsFor(ctx, me.ID)
	themIntents, _ := m.Store.IntentsFor(ctx, them.ID)
	if !IntentsOK(meIntents, themIntents) {
		return false
	}
	meProfile, _ := m.Store.ProfileFor(ctx, me.ID)
	themProfile, _ := m.Store.ProfileFor(ctx, them.ID)
	mePref, _ := m.Store.PreferenceFor(ctx, me.ID)
	themPref, _ := m.Store.PreferenceFor(ctx, them.ID)

	var meAge, themAge *int
	var meGender, themGender *string
	if meProfile != nil {
		meAge, meGender = meProfile.Age, meProfile.GenderID
	}
	if themProfile != nil {
		themAge, themGender = themProfile.Age, themProfile.GenderID
	}
	if !AgeOK(meAge, mePref, themAge, themPref) {
		return false
	}
	if !GenderOK(mePref, themGender) || !GenderOK(themPref, meGender) {
		return false
	}
	return true
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

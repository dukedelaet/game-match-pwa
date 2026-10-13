package httpapi

import (
	"context"
	"net/http"

	"gamematch/internal/store"
)

// matches lists the caller's current mutual matches with their pair and thread.
//
// This was 4 + 6N statements: a pass per row for the block check, the pair, the
// thread and the other user's card, and no LIMIT. Everything the rows share is
// now loaded in bulk and the result set is capped.
func (s *Server) matches(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	limit := listLimit(r)

	rows, err := s.Store.MutualMatchesForLimited(ctx, u.ID, limit)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	items, err := s.matchItems(ctx, u.ID, rows)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// matchItems assembles the match list, loading each shared relation once for the
// whole page rather than once per row.
func (s *Server) matchItems(ctx context.Context, userID string, rows []store.Match) ([]map[string]any, error) {
	items := []map[string]any{}
	if len(rows) == 0 {
		return items, nil
	}

	otherIDs := make([]string, 0, len(rows))
	matchIDs := make([]string, 0, len(rows))
	pairs := make([][2]string, 0, len(rows))
	for _, m := range rows {
		other := m.UserA
		if userID == m.UserA {
			other = m.UserB
		}
		otherIDs = append(otherIDs, other)
		matchIDs = append(matchIDs, m.ID)
		pairs = append(pairs, [2]string{m.UserA, m.UserB})
	}

	blocked, err := s.visibleTo(ctx, userID, otherIDs)
	if err != nil {
		return nil, err
	}
	cards, err := s.cardsFor(ctx, otherIDs)
	if err != nil {
		return nil, err
	}
	threads, err := s.Store.ThreadsForMatches(ctx, matchIDs)
	if err != nil {
		return nil, err
	}
	rel, err := s.Store.PairsForPeople(ctx, pairs)
	if err != nil {
		return nil, err
	}

	for i, m := range rows {
		if blocked[otherIDs[i]] {
			continue
		}
		var pairID, threadID *string
		if p := rel[store.PairKey(m.UserA, m.UserB)]; p != nil {
			id := p.ID
			pairID = &id
		}
		if th := threads[m.ID]; th != nil {
			id := th.ID
			threadID = &id
		}
		items = append(items, map[string]any{
			"id":              m.ID,
			"state":           m.State,
			"originSessionId": m.OriginSessionID,
			"matchedAt":       m.MatchedAt,
			"expiresAt":       m.ExpiresAt,
			"pairId":          pairID,
			"threadId":        threadID,
			"other":           cards[otherIDs[i]],
		})
	}
	return items, nil
}

// meBlocks lists the blocks the caller has placed.
func (s *Server) meBlocks(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	blocks, err := s.Store.BlocksBy(ctx, u.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	items := []map[string]any{}
	for _, b := range blocks {
		card, err := s.card(ctx, b.BlockedID)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
			return
		}
		items = append(items, map[string]any{
			"userId": b.BlockedID,
			"at":     b.CreatedAt,
			"other":  card,
		})
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

// exportMatch is a match as it appears in an account export.
type exportMatch struct {
	ID        string  `json:"id"`
	State     string  `json:"state"`
	MatchedAt *string `json:"matchedAt"`
	ExpiresAt *string `json:"expiresAt"`
}

// exportSession is a session as it appears in an account export.
type exportSession struct {
	ID        string  `json:"id"`
	Kind      string  `json:"kind"`
	Mode      string  `json:"mode"`
	State     string  `json:"state"`
	CreatedAt *string `json:"createdAt"`
	EndedAt   *string `json:"endedAt"`
}

// meExport returns a JSON copy of everything the caller owns. It deliberately
// omits DOB, phone, phone hash, and geohash.
func (s *Server) meExport(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)

	me, err := s.publicMe(ctx, u)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	xpEvents, err := s.Store.RecentXpEvents(ctx, u.ID, 200)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	traits, err := s.Store.TraitIDsFor(ctx, u.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	pairs, err := s.Store.PairsForUser(ctx, u.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	matchRows, err := s.Store.MutualMatchesFor(ctx, u.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	blocks, err := s.Store.BlocksBy(ctx, u.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	sessions, err := s.sessionsForExport(ctx, u.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}

	events := make([]XpEventDTO, 0, len(xpEvents))
	for _, e := range xpEvents {
		events = append(events, XpEventDTO{ID: e.ID, UserID: e.UserID, Kind: e.Kind, Amount: e.Amount, CreatedAt: e.CreatedAt, UpdatedAt: e.UpdatedAt})
	}
	pairIDs := make([]string, 0, len(pairs))
	for _, p := range pairs {
		pairIDs = append(pairIDs, p.ID)
	}
	matchOut := make([]exportMatch, 0, len(matchRows))
	for _, m := range matchRows {
		matchOut = append(matchOut, exportMatch{ID: m.ID, State: m.State, MatchedAt: m.MatchedAt, ExpiresAt: m.ExpiresAt})
	}
	blockedIDs := make([]string, 0, len(blocks))
	for _, b := range blocks {
		blockedIDs = append(blockedIDs, b.BlockedID)
	}

	WriteJSON(w, http.StatusOK, map[string]any{
		"exportedAt":     store.NowTS(),
		"user":           me,
		"traitIds":       traits,
		"xpEvents":       events,
		"pairIds":        pairIDs,
		"matches":        matchOut,
		"sessions":       sessions,
		"blockedUserIds": blockedIDs,
	})
}

// sessionsForExport lists the caller's sessions, newest first.
func (s *Server) sessionsForExport(ctx context.Context, userID string) ([]exportSession, error) {
	rows := []store.GameSession{}
	err := s.Store.DB.SelectContext(ctx, &rows, `
		SELECT gs.* FROM game_sessions gs
		JOIN session_participants sp ON sp.session_id = gs.id
		WHERE sp.user_id = ?
		ORDER BY gs.created_at DESC
		LIMIT 500`, userID)
	if err != nil {
		return nil, err
	}
	out := make([]exportSession, 0, len(rows))
	for _, row := range rows {
		out = append(out, exportSession{
			ID:        row.ID,
			Kind:      row.Kind,
			Mode:      row.Mode,
			State:     row.State,
			CreatedAt: row.CreatedAt,
			EndedAt:   row.EndedAt,
		})
	}
	return out, nil
}

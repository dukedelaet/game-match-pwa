package httpapi

import "net/http"

func (s *Server) home(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	depth, err := s.Store.QueueDepth(ctx, "this_or_that")
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	invites, err := s.Store.PendingInvitesFor(ctx, u.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	pendingPairs, err := s.Store.CountPendingPairs(ctx, u.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	items := []InviteItem{}
	for _, inv := range invites {
		item, err := s.inviteItem(ctx, inv, u.ID)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
			return
		}
		items = append(items, *item)
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"queueDepth":   depth,
		"invites":      items,
		"pendingPairs": pendingPairs,
	})
}

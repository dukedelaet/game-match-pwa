package httpapi

import "net/http"

func (s *Server) queueJoin(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	if u.Incognito {
		WriteError(w, http.StatusConflict, "incognito", "Turn off incognito to enter the lobby")
		return
	}
	var body struct {
		GameKind      string `json:"gameKind"`
		AllowPractice bool   `json:"allowPractice"`
	}
	_ = decodeJSON(r, &body)
	kind := body.GameKind
	if kind == "" {
		kind = "this_or_that"
	}
	if err := s.Matcher.Enqueue(ctx, u.ID, kind, body.AllowPractice); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	status, err := s.Matcher.Tick(ctx, u.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	WriteJSON(w, http.StatusOK, status)
}

func (s *Server) queueStatus(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	status, err := s.Matcher.Tick(r.Context(), u.ID)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	WriteJSON(w, http.StatusOK, status)
}

func (s *Server) queueLeave(w http.ResponseWriter, r *http.Request) {
	u := currentUser(r)
	if err := s.Matcher.Leave(r.Context(), u.ID); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

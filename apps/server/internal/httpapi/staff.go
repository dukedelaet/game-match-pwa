package httpapi

import (
	"net/http"

	"gamematch/internal/store"
)

func (s *Server) allowlist(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	if u.Role != "admin" {
		WriteError(w, http.StatusForbidden, "forbidden", "Staff only")
		return
	}
	if r.Method == http.MethodPost {
		var body struct {
			Phone        string `json:"phone"`
			PublicSignup *bool  `json:"public_signup"`
		}
		_ = decodeJSON(r, &body)
		if body.Phone != "" {
			if err := s.Store.AllowlistAdd(ctx, store.HashPhone(body.Phone)); err != nil {
				WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
				return
			}
		}
		if body.PublicSignup != nil {
			if err := s.Store.SetFlag(ctx, "auth.public_signup", *body.PublicSignup); err != nil {
				WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
				return
			}
		}
	}
	count, err := s.Store.AllowlistCount(ctx)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{
		"publicSignup": s.Store.FlagOn(ctx, "auth.public_signup"),
		"count":        count,
	})
}

func (s *Server) modReports(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	if u.Role != "mod" && u.Role != "admin" {
		WriteError(w, http.StatusForbidden, "forbidden", "Staff only")
		return
	}
	reports, err := s.Store.ReportsNewest(ctx, 100)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	items := make([]ReportDTO, 0, len(reports))
	for _, rep := range reports {
		items = append(items, ReportDTO{
			ID:         rep.ID,
			ReporterID: rep.ReporterID,
			SubjectID:  rep.SubjectID,
			Reason:     rep.Reason,
			Details:    rep.Details,
			Status:     rep.Status,
			CreatedAt:  rep.CreatedAt,
			UpdatedAt:  rep.UpdatedAt,
		})
	}
	WriteJSON(w, http.StatusOK, map[string]any{"items": items})
}

func (s *Server) forcePair(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	if (u.Role != "mod" && u.Role != "admin") || !s.Store.FlagOn(ctx, "staff.force_pair") {
		notFound(w)
		return
	}
	var body struct {
		OtherUserID string `json:"otherUserId"`
		GameKind    string `json:"gameKind"`
	}
	_ = decodeJSON(r, &body)
	kind := body.GameKind
	if kind == "" {
		kind = "this_or_that"
	}
	other, err := s.Store.GetUser(ctx, body.OtherUserID)
	if err != nil {
		notFound(w)
		return
	}
	session, err := s.Engine.StartSession(ctx, kind, "invite", []string{u.ID, other.ID})
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]string{"sessionId": session.ID, "gameKind": kind})
}

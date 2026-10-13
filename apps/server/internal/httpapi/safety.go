package httpapi

import (
	"log"
	"net/http"
	"time"

	"gamematch/internal/game"
	"gamematch/internal/store"
)

func (s *Server) block(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	var body struct {
		UserID string `json:"userId"`
	}
	_ = decodeJSON(r, &body)
	if body.UserID == "" {
		WriteError(w, http.StatusUnprocessableEntity, "bad", "Who?")
		return
	}
	if err := s.Store.InsertBlock(ctx, u.ID, body.UserID); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	// Blocking cancels any pending invites in both directions (§Block).
	if err := s.Store.CancelPendingInvites(ctx, u.ID, body.UserID); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	a, b := game.OrderedPair(u.ID, body.UserID)
	if pair, _ := s.Store.PairFor(ctx, a, b); pair != nil {
		pair.State = "blocked"
		if err := s.Store.UpsertPair(ctx, pair); err != nil {
			WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
			return
		}
	}
	if match, _ := s.Store.MatchFor(ctx, a, b); match != nil {
		if err := s.Store.PutMatchState(ctx, match.ID, "blocked", nil); err != nil {
			WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
			return
		}
		if err := s.Store.PutThreadStateByMatch(ctx, match.ID, "locked"); err != nil {
			WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
			return
		}
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) unblock(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	other := idParam(r)
	if err := s.Store.DeleteBlock(ctx, u.ID, other); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	stillBlocked, err := s.Store.BlockedEitherWay(ctx, u.ID, other)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	if !stillBlocked {
		a, b := game.OrderedPair(u.ID, other)
		if pair, _ := s.Store.PairFor(ctx, a, b); pair != nil {
			pair.State = "unmatched"
			cd := store.FmtTS(time.Now().UTC().Add(14 * 24 * time.Hour))
			pair.CooldownUntil = &cd
			if err := s.Store.UpsertPair(ctx, pair); err != nil {
				WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
				return
			}
		}
		if match, _ := s.Store.MatchFor(ctx, a, b); match != nil {
			if err := s.Store.PutMatchState(ctx, match.ID, "unmatched", nil); err != nil {
				WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
				return
			}
		}
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) report(w http.ResponseWriter, r *http.Request) {
	ctx := r.Context()
	u := currentUser(r)
	var body struct {
		UserID  string `json:"userId"`
		Reason  string `json:"reason"`
		Details string `json:"details"`
	}
	_ = decodeJSON(r, &body)
	if body.Reason == "" {
		body.Reason = "other"
	}
	// Reports of csam/underage are never dropped (§Reports); everything else is
	// limited to 10/day per reporter.
	if body.Reason != "csam" && body.Reason != "underage" {
		if !s.allow(w, r, "reports:"+u.ID, limitReportsPerUser, windowReports) {
			return
		}
	}
	var subject *string
	if body.UserID != "" {
		subject = &body.UserID
	}
	var details *string
	if body.Details != "" {
		details = &body.Details
	}
	rep := &store.Report{
		ReporterID: u.ID,
		SubjectID:  subject,
		Reason:     body.Reason,
		Details:    details,
		Status:     "open",
	}
	if err := s.Store.InsertReport(ctx, rep); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	if (body.Reason == "csam" || body.Reason == "underage") && body.UserID != "" {
		if body.UserID == u.ID {
			// Reporting yourself should not be a route to the automatic hide.
			WriteError(w, http.StatusUnprocessableEntity, "bad_report", "You cannot report yourself")
			return
		}
		if _, err := s.Store.GetUser(ctx, body.UserID); err != nil {
			// Do not confirm or deny that the account exists.
			notFound(w)
			return
		}
		// The report itself is never dropped and never rate limited -- dropping a
		// real CSAM report is the worse failure. What is limited is the automated
		// hide, which is what a reporter can weaponise: without this, one account
		// could file unlimited reports and sweep any number of people out of
		// matchmaking. Reports past the limit are still filed and still reach the
		// moderation queue; they just wait for a human instead of acting at once.
		//
		// Two budgets: one per reporter (how many accounts one person may knock
		// out of rotation) and one per subject (so N reports about one person
		// cannot produce N hide actions).
		switch {
		case !s.withinLimit(r, "hide:"+u.ID, limitHidesPerReporter, windowHides):
			log.Printf("reports: reporter %s exceeded the automatic-hide budget; report queued for review", u.ID)
		case !s.withinLimit(r, "hide:subject:"+body.UserID, limitHidesPerSubject, windowHides):
			log.Printf("reports: subject %s already hidden by automation; review queued", body.UserID)
		default:
			if err := s.Store.SetUserStatus(ctx, body.UserID, "hidden"); err != nil {
				WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
				return
			}
		}
		reportID := rep.ID
		purge := store.FmtTS(time.Now().UTC().Add(90 * 24 * time.Hour))
		hold := &store.LegalHold{
			UserID:     body.UserID,
			ReportID:   &reportID,
			Reason:     body.Reason,
			Status:     "active",
			PurgeAfter: &purge,
		}
		if err := s.Store.InsertLegalHold(ctx, hold); err != nil {
			WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
			return
		}
	}
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

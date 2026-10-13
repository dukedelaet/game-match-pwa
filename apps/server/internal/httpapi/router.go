package httpapi

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

// maxJSONBody caps JSON request bodies. decodeJSON is otherwise unbounded, so
// an endpoint that accepts a body will read however many bytes the client sends
// and allocate them as Go strings -- one unauthenticated request to
// /auth/otp/start with a multi-gigabyte phone field is enough to OOM the
// process. Multipart photo upload replaces this cap with a larger one.
const maxJSONBody = 64 << 10 // 64 KiB

// LimitBody rejects request bodies larger than n with 413 before any handler
// allocates them.
func LimitBody(n int64) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if r.ContentLength > n {
				WriteError(w, http.StatusRequestEntityTooLarge, "too_large", "That request is too large")
				return
			}
			if r.Body != nil {
				r.Body = http.MaxBytesReader(w, r.Body, n)
			}
			next.ServeHTTP(w, r)
		})
	}
}

// Router builds the /v1 route table.
func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Use(Recover)
	r.Use(LimitBody(maxJSONBody))
	if s.Cfg.Env != "test" {
		r.Use(LogRequests)
	}

	r.Route("/v1", func(r chi.Router) {
		r.Get("/healthz", s.health)
		r.Get("/catalogs", s.catalogs)
		r.Get("/legal", s.legal)
		r.Get("/games", s.games)

		r.Post("/auth/otp/start", s.otpStart)
		r.Post("/auth/otp/verify", s.otpVerify)
		r.Post("/auth/oauth/{provider}", s.oauthDemo)
		r.Post("/auth/logout", s.logout)
		r.Get("/auth/session", s.sessionEndpoint)

		r.Group(func(r chi.Router) {
			r.Use(s.RequireUser)

			r.Get("/me", s.getMe)
			r.Patch("/me", s.patchMe)
			r.Delete("/me", s.deleteMe)
			r.Post("/me/photos", s.uploadPhoto)
			r.Get("/me/xp", s.meXp)
			r.Get("/me/blocks", s.meBlocks)
			r.Get("/me/export", s.meExport)
			r.Post("/me/location", s.setLocation)
			r.Get("/push/vapid-public-key", s.vapidPublicKey)
			r.Post("/me/push", s.subscribePush)
			r.Delete("/me/push", s.unsubscribePush)
			r.Get("/photos/{id}", s.photo)

			r.Get("/home", s.home)

			r.Post("/queue", s.idempotent("POST /queue", s.queueJoin))
			r.Get("/queue/status", s.queueStatus)
			r.Delete("/queue", s.queueLeave)

			r.Post("/sessions/{id}/join", s.sessionJoin)
			r.Get("/sessions/{id}", s.sessionShow)
			r.Post("/sessions/{id}/answer", s.sessionAnswer)
			r.Post("/sessions/{id}/leave", s.sessionLeave)
			r.Post("/sessions/{id}/rematch", s.rematch)

			r.Get("/matches", s.matches)

			r.Get("/pairs", s.pairs)
			r.Get("/pairs/{id}", s.pairShow)
			r.Post("/pairs/{id}/connect", s.idempotent("POST /pairs/:id/connect", s.pairAct))
			r.Post("/pairs/{id}/unmatch", s.unmatch)

			r.Get("/threads", s.threads)
			r.Get("/threads/{id}/messages", s.messages)
			r.Post("/threads/{id}/messages", s.idempotent("POST /threads/:id/messages", s.sendMessage))

			r.Post("/invites", s.idempotent("POST /invites", s.inviteCreate))
			r.Post("/invites/{id}/accept", s.inviteAccept)
			r.Post("/invites/{id}/decline", s.inviteDecline)

			r.Post("/blocks", s.block)
			r.Delete("/blocks/{id}", s.unblock)
			r.Post("/reports", s.report)

			r.Get("/internal/mod/reports", s.modReports)
			r.Post("/internal/force-pair", s.forcePair)
			r.Get("/staff/allowlist", s.allowlist)
			r.Post("/staff/allowlist", s.allowlist)
		})
	})
	return r
}

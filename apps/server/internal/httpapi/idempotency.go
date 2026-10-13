package httpapi

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"log"
	"net/http"

	"gamematch/internal/store"
)

const maxIdempotentBody = 1 << 20 // 1 MiB

// idempotent wraps a write handler so a retried request carrying the same
// Idempotency-Key and body replays the original response instead of acting
// twice. Requests without the header pass straight through.
func (s *Server) idempotent(endpoint string, next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		key := r.Header.Get("Idempotency-Key")
		if key == "" || len(key) > 200 {
			next(w, r)
			return
		}

		body, err := io.ReadAll(io.LimitReader(r.Body, maxIdempotentBody))
		_ = r.Body.Close()
		if err != nil {
			WriteError(w, http.StatusBadRequest, "bad", "Could not read the request")
			return
		}
		r.Body = io.NopCloser(bytes.NewReader(body))

		u := currentUser(r)
		hash := sha256.Sum256(append([]byte(r.Method+" "+r.URL.Path+"\x00"), body...))
		hashHex := hex.EncodeToString(hash[:])

		var stored struct {
			RequestHash string `db:"request_hash"`
			Status      int    `db:"status"`
			Response    string `db:"response"`
		}

		// Claim the key first, then act. Checking for an existing row and only
		// inserting afterwards left a window in which two concurrent requests
		// with the same key both saw nothing and both ran the handler, so a
		// double-tapped Connect made two matches and a retried message was sent
		// twice. ON CONFLICT DO NOTHING could not help: it was already too late.
		//
		// status 0 marks a claim with no recorded response yet. A duplicate that
		// arrives before the original finishes gets 409 rather than an empty
		// replay, because there is nothing to replay.
		res, err := s.Store.DB.ExecContext(r.Context(), `
			INSERT INTO idempotency_keys (user_id, key, endpoint, request_hash, status, response, created_at)
			VALUES (?,?,?,?,0,'',?)
			ON CONFLICT(user_id, key) DO NOTHING`,
			u.ID, key, endpoint, hashHex, store.NowTS())
		if err != nil {
			log.Printf("idempotency claim: %v", err)
			// Fail open rather than dropping the request if the claim table is
			// unavailable; the handler is still the source of truth.
			rec := &captureWriter{header: http.Header{}}
			next(rec, r)
			rec.flush(w)
			return
		}
		claimed, _ := res.RowsAffected()

		if claimed == 0 {
			// Someone else owns this key. Replay their recorded response, or tell
			// the caller to retry if it is not ready yet.
			err = s.Store.DB.GetContext(r.Context(), &stored,
				`SELECT request_hash, status, response FROM idempotency_keys WHERE user_id = ? AND key = ?`,
				u.ID, key)
			if err == nil && stored.RequestHash != hashHex {
				WriteError(w, http.StatusConflict, "idempotency_conflict",
					"That idempotency key was used with a different request")
				return
			}
			if err == nil && stored.Status >= 100 {
				w.Header().Set("Content-Type", "application/json")
				w.Header().Set("Idempotency-Replayed", "true")
				w.WriteHeader(stored.Status)
				_, _ = w.Write([]byte(stored.Response))
				return
			}
			WriteError(w, http.StatusConflict, "idempotency_in_progress",
				"An identical request is still being processed")
			return
		}

		rec := &captureWriter{header: http.Header{}}
		next(rec, r)

		if rec.status >= 200 && rec.status < 300 {
			if _, err := s.Store.DB.ExecContext(r.Context(), `
				UPDATE idempotency_keys SET status=?, response=?, created_at=?
				WHERE user_id=? AND key=?`,
				rec.status, rec.body.String(), store.NowTS(), u.ID, key); err != nil {
				log.Printf("idempotency store: %v", err)
			}
		} else {
			// Not a conclusive success: release the key so the caller may retry.
			if _, err := s.Store.DB.ExecContext(r.Context(),
				`DELETE FROM idempotency_keys WHERE user_id=? AND key=? AND status=0`,
				u.ID, key); err != nil {
				log.Printf("idempotency release: %v", err)
			}
		}
		rec.flush(w)
	}
}

// captureWriter records a handler's response so it can be stored and replayed.
type captureWriter struct {
	header http.Header
	status int
	body   bytes.Buffer
}

func (c *captureWriter) Header() http.Header { return c.header }

func (c *captureWriter) WriteHeader(status int) {
	if c.status == 0 {
		c.status = status
	}
}

func (c *captureWriter) Write(b []byte) (int, error) {
	if c.status == 0 {
		c.status = http.StatusOK
	}
	return c.body.Write(b)
}

func (c *captureWriter) flush(w http.ResponseWriter) {
	header := w.Header()
	for k, values := range c.header {
		for _, v := range values {
			header.Add(k, v)
		}
	}
	status := c.status
	if status == 0 {
		status = http.StatusOK
	}
	w.WriteHeader(status)
	_, _ = w.Write(c.body.Bytes())
}

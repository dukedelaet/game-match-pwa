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
		err = s.Store.DB.GetContext(r.Context(), &stored,
			`SELECT request_hash, status, response FROM idempotency_keys WHERE user_id = ? AND key = ?`,
			u.ID, key)
		if err == nil {
			if stored.RequestHash != hashHex {
				WriteError(w, http.StatusConflict, "idempotency_conflict",
					"That idempotency key was used with a different request")
				return
			}
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Idempotency-Replayed", "true")
			w.WriteHeader(stored.Status)
			_, _ = w.Write([]byte(stored.Response))
			return
		}

		rec := &captureWriter{header: http.Header{}}
		next(rec, r)

		// Only conclusive successes are remembered; a retry of a 4xx should be
		// allowed to try again.
		if rec.status >= 200 && rec.status < 300 {
			if _, err := s.Store.DB.ExecContext(r.Context(), `
				INSERT INTO idempotency_keys (user_id, key, endpoint, request_hash, status, response, created_at)
				VALUES (?,?,?,?,?,?,?)
				ON CONFLICT(user_id, key) DO NOTHING`,
				u.ID, key, endpoint, hashHex, rec.status, rec.body.String(), store.NowTS()); err != nil {
				log.Printf("idempotency store: %v", err)
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

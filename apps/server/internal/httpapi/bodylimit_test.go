package httpapi

import (
	"bytes"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/stretchr/testify/require"
)

// decodeJSON is unbounded, so any endpoint that accepts a JSON body will read
// however many bytes the client sends. An unauthenticated POST /auth/otp/start
// carrying a multi-gigabyte phone string allocated it in full before validation.
func TestOversizedJSONBodyIsRejected(t *testing.T) {
	s, _ := newTestServer(t)
	c := newClient(t, s)

	body := `{"phone":"` + strings.Repeat("1", 2<<20) + `"}`
	rec := c.reqRaw(http.MethodPost, "/v1/auth/otp/start", bytes.NewBufferString(body))

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code,
		"a 2 MiB JSON body must be rejected, not parsed")
}

func TestOversizedBodyOnAuthenticatedEndpoint(t *testing.T) {
	s, _ := newTestServer(t)
	c := login(t, s, "+15551111111")

	body := `{"name":"` + strings.Repeat("x", 2<<20) + `"}`
	rec := c.reqRaw(http.MethodPatch, "/v1/me", bytes.NewBufferString(body))

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

// A body just under the cap must still be parsed, or the limit is set too low
// for the app's real payloads.
func TestNormalSizedJSONBodyIsStillAccepted(t *testing.T) {
	s, _ := newTestServer(t)
	c := newClient(t, s)

	rec := c.reqRaw(http.MethodPost, "/v1/auth/otp/start",
		bytes.NewBufferString(`{"phone":"+15551234567"}`))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// The photo upload allows far more than the JSON cap, so the global limit must
// not truncate a legitimate upload.
func TestPhotoUploadAllowsMoreThanTheJSONCap(t *testing.T) {
	s, _ := newTestServer(t)
	c := login(t, s, "+15551111111")

	rec := c.upload("/v1/me/photos", "photo", "me.png", pngBytes(t, 2000, 1200))
	require.Equal(t, http.StatusOK, rec.Code, rec.Body.String())
}

// reqRaw sends an already-encoded body, for payloads too large to marshal from
// a struct.
func (c *client) reqRaw(method, path string, body io.Reader) *httptest.ResponseRecorder {
	c.t.Helper()
	req := httptest.NewRequest(method, path, body)
	req.Header.Set("Content-Type", "application/json")
	if c.cookie != nil {
		req.AddCookie(c.cookie)
	}
	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)
	for _, ck := range rec.Result().Cookies() {
		if ck.Name == sessionCookie {
			c.cookie = ck
		}
	}
	return rec
}

// A chunked request has no ContentLength, so the cheap header check cannot see
// it. MaxBytesReader is what bounds those, and decodeJSON must surface the
// failure rather than letting the handler continue with a zero-value struct.
func TestChunkedOversizedBodyIsBounded(t *testing.T) {
	s, _ := newTestServer(t)
	c := newClient(t, s)

	req := httptest.NewRequest(http.MethodPost, "/v1/auth/otp/start",
		io.NopCloser(bytes.NewBufferString(`{"phone":"`+strings.Repeat("1", 2<<20)+`"}`)))
	req.ContentLength = -1 // force chunked semantics
	req.Header.Set("Content-Type", "application/json")

	rec := httptest.NewRecorder()
	c.h.ServeHTTP(rec, req)

	require.Equal(t, http.StatusRequestEntityTooLarge, rec.Code)
}

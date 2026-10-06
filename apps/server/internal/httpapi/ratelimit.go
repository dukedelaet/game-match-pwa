package httpapi

import (
	"log"
	"math"
	"net"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// Rate limits, from §Rate limits of the design doc.
const (
	limitOTPStartPerPhone  = 3
	limitOTPStartPerIP     = 5
	limitOTPVerifyPerPhone = 5
	windowOTP              = 15 * time.Minute

	// A challenge is required after this many OTP failures from one IP.
	limitOTPFailuresPerIP = 2

	limitQueuePerUser = 5
	windowQueue       = time.Minute

	limitInvitesPerUser = 10
	windowInvites       = time.Hour

	limitMessagesPerThread = 20
	windowMessages         = time.Minute

	limitReportsPerUser = 10
	windowReports       = 24 * time.Hour

	limitPhotosPerUser = 12
	windowPhotos       = time.Hour

	limitSessionPollPerUser = 2
	windowSessionPoll       = time.Second
)

// allow records an attempt and reports whether the caller may proceed. When it
// may not, it writes a 429 with Retry-After and returns false.
func (s *Server) allow(w http.ResponseWriter, r *http.Request, bucket string, limit int, window time.Duration) bool {
	if s.DisableRateLimits {
		return true
	}
	ok, retry, err := s.Limiter.Allow(r.Context(), bucket, limit, window)
	if err != nil {
		log.Printf("ratelimit %s: %v", bucket, err)
		return true // fail open: a broken limiter must not take the app down
	}
	if ok {
		return true
	}
	seconds := int(math.Ceil(retry.Seconds()))
	if seconds < 1 {
		seconds = 1
	}
	w.Header().Set("Retry-After", strconv.Itoa(seconds))
	WriteError(w, http.StatusTooManyRequests, "rate_limited", "Too many requests, try again shortly")
	return false
}

// withinLimit records an attempt without writing a response, for flows where
// the caller must stay silent (OTP start must not reveal anything).
func (s *Server) withinLimit(r *http.Request, bucket string, limit int, window time.Duration) bool {
	if s.DisableRateLimits {
		return true
	}
	ok, _, err := s.Limiter.Allow(r.Context(), bucket, limit, window)
	if err != nil {
		log.Printf("ratelimit %s: %v", bucket, err)
		return true
	}
	return ok
}

// turnstileOK decides whether a challenge is needed and, if so, whether the
// supplied token passes. With no secret configured this always succeeds.
func (s *Server) turnstileOK(r *http.Request, ip, token string) bool {
	failures, err := s.Limiter.Count(r.Context(), "otp:fail:ip:"+ip, windowOTP)
	if err != nil {
		failures = 0
	}
	if failures <= limitOTPFailuresPerIP {
		return true
	}
	ok, err := s.Turnstile.Verify(r.Context(), token, ip)
	if err != nil {
		log.Printf("turnstile: %v", err)
		return false
	}
	if ok {
		_ = s.Limiter.Reset(r.Context(), "otp:fail:ip:"+ip)
	}
	return ok
}

// clientIP prefers the proxy header set by Caddy, falling back to the socket.
func clientIP(r *http.Request) string {
	if forwarded := r.Header.Get("X-Forwarded-For"); forwarded != "" {
		parts := strings.Split(forwarded, ",")
		return strings.TrimSpace(parts[0])
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

package httpapi

import (
	"crypto/rand"
	"encoding/json"
	"fmt"
	"log"
	"math/big"
	"net/http"
	"regexp"
	"time"

	"github.com/go-chi/chi/v5"

	"gamematch/internal/config"
	"gamematch/internal/game"
	"gamematch/internal/ratelimit"
	"gamematch/internal/sms"
	"gamematch/internal/store"
	"gamematch/internal/turnstile"
)

const (
	otpTTL     = 15 * time.Minute
	sessionTTL = 2 * time.Hour
)

// Server wires the data layer and game engine into HTTP handlers.
type Server struct {
	Store     *store.Store
	Engine    *game.Engine
	Matcher   *game.Matcher
	Cfg       config.Config
	PhotosDir string

	Limiter   *ratelimit.Limiter
	SMS       sms.Provider
	Turnstile turnstile.Verifier

	// DisableRateLimits bypasses throttling. Tests set it so poll-heavy flows
	// do not have to sleep; the limiter has its own tests.
	DisableRateLimits bool
}

// NewServer builds a Server.
func NewServer(st *store.Store, engine *game.Engine, matcher *game.Matcher, cfg config.Config) *Server {
	return &Server{
		Store:     st,
		Engine:    engine,
		Matcher:   matcher,
		Cfg:       cfg,
		PhotosDir: cfg.PhotosDir(),
		Limiter:   ratelimit.New(st.DB),
		SMS:       sms.New(cfg),
		Turnstile: turnstile.New(cfg.TurnstileSecret),
	}
}

var phoneRE = regexp.MustCompile(`^\+?[0-9]{10,15}$`)

func (s *Server) health(w http.ResponseWriter, r *http.Request) {
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func decodeJSON(r *http.Request, v any) error {
	defer func() { _ = r.Body.Close() }()
	dec := json.NewDecoder(r.Body)
	return dec.Decode(v)
}

func randomCode() string {
	n, err := rand.Int(rand.Reader, big.NewInt(1000000))
	if err != nil {
		return "000000"
	}
	return fmt.Sprintf("%06d", n.Int64())
}

func (s *Server) otpStart(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone          string `json:"phone"`
		TurnstileToken string `json:"turnstileToken"`
	}
	_ = decodeJSON(r, &body)
	ip := clientIP(r)

	// A challenge is demanded only after repeated failures from one IP, and
	// only when Turnstile is configured.
	if !s.turnstileOK(r, ip, body.TurnstileToken) {
		WriteError(w, http.StatusForbidden, "captcha_required", "Complete the challenge and try again")
		return
	}

	// OTP start always answers {ok:true} so it cannot be used to probe which
	// phone numbers exist; limits are enforced silently.
	if !s.withinLimit(r, "otp:start:ip:"+ip, limitOTPStartPerIP, windowOTP) {
		WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	phone := store.NormalizePhone(body.Phone)
	if !phoneRE.MatchString(phone) {
		WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if !s.withinLimit(r, "otp:start:phone:"+phone, limitOTPStartPerPhone, windowOTP) {
		WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
		return
	}
	if !s.Store.FlagOn(r.Context(), "auth.public_signup") {
		hash := store.HashPhone(phone)
		ok, err := s.Store.AllowlistHas(r.Context(), hash)
		if err != nil {
			WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
			return
		}
		if !ok {
			WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
			return
		}
	}
	code := s.Cfg.DevOTP
	if !s.Cfg.Debug {
		code = randomCode()
	}
	if err := s.Store.PutOTP(r.Context(), phone, code, otpTTL); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	// The kill switch flag disables outbound SMS during an incident.
	if s.Store.FlagOn(r.Context(), "auth.otp") {
		if err := s.SMS.Send(r.Context(), phone, "Your GameMatch code is "+code); err != nil {
			log.Printf("sms send: %v", err)
		}
	}
	out := map[string]any{"ok": true}
	if s.Cfg.Debug {
		out["devCode"] = code
	}
	WriteJSON(w, http.StatusOK, out)
}

func (s *Server) otpVerify(w http.ResponseWriter, r *http.Request) {
	var body struct {
		Phone string `json:"phone"`
		Code  string `json:"code"`
	}
	_ = decodeJSON(r, &body)
	phone := store.NormalizePhone(body.Phone)
	ip := clientIP(r)

	if !s.allow(w, r, "otp:verify:phone:"+phone, limitOTPVerifyPerPhone, windowOTP) {
		return
	}

	expected, ok := s.Store.TakeOTP(r.Context(), phone)
	if !ok || body.Code != expected {
		// Count failures per IP so a challenge can be demanded at OTP start.
		s.Limiter.Bump(r.Context(), "otp:fail:ip:"+ip, windowOTP)
		WriteError(w, http.StatusUnauthorized, "invalid", "That code did not match")
		return
	}
	_ = s.Limiter.Reset(r.Context(), "otp:fail:ip:"+ip)
	hash := store.HashPhone(phone)
	u, err := s.Store.UserByPhoneHash(r.Context(), hash)
	if err != nil {
		newUser := store.User{
			PhoneE164Hash:  &hash,
			Status:         "pending",
			OnboardingStep: "intent",
			Role:           "user",
			Level:          1,
		}
		if err := s.Store.InsertUser(r.Context(), &newUser); err != nil {
			WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
			return
		}
		_ = s.Store.UpsertPrivatePhone(r.Context(), newUser.ID, phone)
		_ = s.Store.UpsertPreference(r.Context(), store.Preference{
			UserID:        newUser.ID,
			AgeMin:        18,
			AgeMax:        99,
			DistanceScope: "metro",
			WhoToMeetOpen: true,
		})
		u = newUser
	}
	if err := s.login(w, r, u.ID); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	_ = s.Store.TouchPresence(r.Context(), u.ID)
	u, _ = s.Store.GetUser(r.Context(), u.ID)
	me, err := s.publicMe(r.Context(), u)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"user": me})
}

func (s *Server) oauthDemo(w http.ResponseWriter, r *http.Request) {
	provider := chi.URLParam(r, "provider")
	if provider != "apple" && provider != "google" {
		WriteError(w, http.StatusUnprocessableEntity, "bad", "Unknown sign-in")
		return
	}
	if !s.Cfg.Debug {
		WriteError(w, http.StatusNotImplemented, "oauth", "Sign-in is not configured yet. Use a phone code.")
		return
	}
	email := provider + "-demo@gamematch.local"
	u, err := s.Store.UserByEmail(r.Context(), email)
	if err != nil {
		name := "Google Demo"
		if provider == "apple" {
			name = "Apple Demo"
		}
		u = store.User{
			Email:          &email,
			Name:           &name,
			Status:         "pending",
			OnboardingStep: "intent",
			Role:           "user",
			Level:          1,
		}
		if err := s.Store.InsertUser(r.Context(), &u); err != nil {
			WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
			return
		}
		_ = s.Store.UpsertPreference(r.Context(), store.Preference{
			UserID:        u.ID,
			AgeMin:        18,
			AgeMax:        99,
			DistanceScope: "metro",
			WhoToMeetOpen: true,
		})
	}
	if err := s.login(w, r, u.ID); err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	me, err := s.publicMe(r.Context(), u)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"user": me})
}

func (s *Server) logout(w http.ResponseWriter, r *http.Request) {
	if token := sessionToken(r); token != "" {
		_ = s.Store.DeleteSession(r.Context(), token)
	}
	s.clearSessionCookie(w)
	WriteJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) sessionEndpoint(w http.ResponseWriter, r *http.Request) {
	uid, ok := s.Store.SessionUser(r.Context(), sessionToken(r))
	if !ok {
		WriteError(w, http.StatusUnauthorized, "unauth", "Sign in")
		return
	}
	u, err := s.Store.GetUser(r.Context(), uid)
	if err != nil {
		WriteError(w, http.StatusUnauthorized, "unauth", "Sign in")
		return
	}
	_ = s.Store.TouchPresence(r.Context(), uid)
	u, _ = s.Store.GetUser(r.Context(), uid)
	me, err := s.publicMe(r.Context(), u)
	if err != nil {
		WriteError(w, http.StatusInternalServerError, "server", "Something went wrong")
		return
	}
	WriteJSON(w, http.StatusOK, map[string]any{"user": me})
}

func (s *Server) login(w http.ResponseWriter, r *http.Request, userID string) error {
	token, err := s.Store.CreateSession(r.Context(), userID, sessionTTL)
	if err != nil {
		return err
	}
	s.setSessionCookie(w, token, sessionTTL)
	return nil
}

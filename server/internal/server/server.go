package server

import (
	_ "embed"
	"html/template"
	"io"
	"log"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"

	"camserver/internal/auth"
	"camserver/internal/config"
	"camserver/internal/hub"
)

//go:embed web/index.html
var pageHTML string

const loginAttempts = 8

type pageData struct {
	LoggedIn bool
	Error    string
}

// New returns the HTTP handler for login, viewing, and camera ingest.
func New(cfg config.Config, h *hub.Hub) http.Handler {
	s := &app{
		cfg:   cfg,
		hub:   h,
		limit: auth.NewLimiter(loginAttempts, time.Minute),
		tmpl:  template.Must(template.New("page").Parse(pageHTML)),
	}
	mux := http.NewServeMux()
	mux.HandleFunc("GET /", s.home)
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /logout", s.logout)
	mux.HandleFunc("GET /frame", s.frame)
	mux.HandleFunc("POST /ingest", s.ingest)
	return mux
}

type app struct {
	cfg          config.Config
	hub          *hub.Hub
	limit        *auth.Limiter
	tmpl         *template.Template
	logMu        sync.Mutex
	lastFrameLog time.Time
}

func (s *app) home(w http.ResponseWriter, r *http.Request) {
	s.render(w, http.StatusOK, pageData{LoggedIn: s.authorized(r)})
}

func (s *app) login(w http.ResponseWriter, r *http.Request) {
	r.Body = http.MaxBytesReader(w, r.Body, 1<<12)
	if err := r.ParseForm(); err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	ip := clientIP(r)
	now := time.Now()
	if s.limit.Blocked(ip, now) {
		http.Error(w, "too many attempts", http.StatusTooManyRequests)
		return
	}
	if !auth.SecretOK(r.PostFormValue("password"), s.cfg.ViewPassword) {
		s.limit.Fail(ip, now)
		s.render(w, http.StatusUnauthorized, pageData{Error: "Wrong password"})
		return
	}
	token, exp := auth.IssueToken(s.cfg.SessionSecret, now)
	http.SetCookie(w, sessionCookie(token, exp, r))
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *app) logout(w http.ResponseWriter, r *http.Request) {
	http.SetCookie(w, &http.Cookie{
		Name:     auth.CookieName,
		Value:    "",
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secureCookie(r),
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
	})
	http.Redirect(w, r, "/", http.StatusSeeOther)
}

func (s *app) frame(w http.ResponseWriter, r *http.Request) {
	if !s.authorized(r) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	frame := s.hub.Latest()
	if frame == nil {
		http.Error(w, "no frame yet", http.StatusNoContent)
		return
	}
	w.Header().Set("Content-Type", "image/jpeg")
	w.Header().Set("Cache-Control", "no-store")
	_, _ = w.Write(frame)
}

func (s *app) ingest(w http.ResponseWriter, r *http.Request) {
	if !auth.SecretOK(r.Header.Get("X-API-Key"), s.cfg.IngestAPIKey) {
		http.Error(w, "unauthorized", http.StatusUnauthorized)
		return
	}
	r.Body = http.MaxBytesReader(w, r.Body, hub.MaxFrameBytes)
	data, err := io.ReadAll(r.Body)
	if err != nil {
		http.Error(w, "bad request", http.StatusBadRequest)
		return
	}
	if err := s.hub.Publish(data); err != nil {
		http.Error(w, "invalid jpeg", http.StatusBadRequest)
		return
	}
	s.logIngest("ingest frame %d bytes", len(data))
	w.WriteHeader(http.StatusNoContent)
}

func (s *app) logIngest(format string, n int) {
	s.logMu.Lock()
	defer s.logMu.Unlock()
	if time.Since(s.lastFrameLog) < 2*time.Second {
		return
	}
	s.lastFrameLog = time.Now()
	log.Printf(format, n)
}

func (s *app) authorized(r *http.Request) bool {
	c, err := r.Cookie(auth.CookieName)
	if err != nil {
		return false
	}
	return auth.ValidToken(s.cfg.SessionSecret, c.Value, time.Now())
}

func (s *app) render(w http.ResponseWriter, status int, data pageData) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.WriteHeader(status)
	if err := s.tmpl.Execute(w, data); err != nil {
		log.Printf("render: %v", err)
	}
}

func sessionCookie(token string, exp time.Time, r *http.Request) *http.Cookie {
	return &http.Cookie{
		Name:     auth.CookieName,
		Value:    token,
		Path:     "/",
		HttpOnly: true,
		SameSite: http.SameSiteLaxMode,
		Secure:   secureCookie(r),
		Expires:  exp,
	}
}

func secureCookie(r *http.Request) bool {
	return r.TLS != nil || strings.EqualFold(r.Header.Get("X-Forwarded-Proto"), "https")
}

func clientIP(r *http.Request) string {
	if ip := strings.TrimSpace(r.Header.Get("CF-Connecting-IP")); ip != "" {
		return ip
	}
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

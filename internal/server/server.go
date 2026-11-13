// Package server holds the HTTP handlers of the URL shortener.
package server

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"expvar"
	"net/http"
	"net/url"
	"strconv"
	"time"

	"shortener/internal/metrics"
	"shortener/internal/session"
)

// Links is where short links are saved and found: the database, or the database behind a cache.
type Links interface {
	Create(ctx context.Context, url string) (string, error)
	Lookup(ctx context.Context, code string) (url string, ok bool, err error)
	Delete(ctx context.Context, code string) error
}

// Events is the queue: work that can wait is added here and done later by the worker.
type Events interface {
	Publish(ctx context.Context, lane, id string, fields map[string]string) error
}

// Limiter says whether a client may make another request now, or how long it must wait.
type Limiter interface {
	Allow(ctx context.Context, client string) (ok bool, wait time.Duration, err error)
}

var opens = expvar.NewInt("opens") // opens answered, shown at /debug/vars

// Server wires the handlers to their dependencies.
type Server struct {
	Sessions session.Store
	Links    Links
	Events   Events
	Limiter  Limiter          // nil: no limit
	Metrics  *metrics.Metrics // nil: no metrics
}

// Handler returns the routes:
//
//	POST /login      start a session (demo only: no password)
//	POST /links      create a short link (needs a session)
//	GET  /{code}     open a short link (302 to the long URL)
//	GET  /healthz    "ok" while the app answers (the load balancer checks it)
//	GET  /metrics    latency, traffic, errors and saturation, for a scraper
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /links", s.create)
	mux.HandleFunc("GET /{code}", s.open)
	mux.Handle("GET /debug/vars", expvar.Handler())
	// Kept shallow on purpose: it says the app is answering. The load balancer calls it every second.
	mux.HandleFunc("GET /healthz", func(w http.ResponseWriter, r *http.Request) { _, _ = w.Write([]byte("ok\n")) })
	if s.Metrics == nil {
		return mux
	}
	mux.Handle("GET /metrics", s.Metrics.Handler())
	return s.Metrics.Wrap(mux)
}

func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	var in struct {
		User string `json:"user"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil || in.User == "" {
		http.Error(w, "need a user", http.StatusBadRequest)
		return
	}
	id, err := s.Sessions.Create(r.Context(), in.User)
	if err != nil {
		http.Error(w, "session store down", http.StatusServiceUnavailable)
		return
	}
	http.SetCookie(w, &http.Cookie{Name: "sid", Value: id, Path: "/", HttpOnly: true})
	w.WriteHeader(http.StatusNoContent)
}

func (s *Server) create(w http.ResponseWriter, r *http.Request) {
	c, err := r.Cookie("sid")
	if err != nil {
		http.Error(w, "log in first", http.StatusUnauthorized)
		return
	}
	user, ok, err := s.Sessions.User(r.Context(), c.Value)
	if err != nil {
		http.Error(w, "session store down", http.StatusServiceUnavailable)
		return
	}
	if !ok {
		http.Error(w, "unknown session", http.StatusUnauthorized)
		return
	}
	if s.tooMany(w, r, user) {
		return
	}
	var in struct {
		URL string `json:"url"`
	}
	if json.NewDecoder(r.Body).Decode(&in) != nil {
		http.Error(w, "bad body", http.StatusBadRequest)
		return
	}
	if u, err := url.ParseRequestURI(in.URL); err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		http.Error(w, "need an http(s) URL", http.StatusBadRequest)
		return
	}
	code, err := s.Links.Create(r.Context(), in.URL)
	if err != nil {
		http.Error(w, "could not save", http.StatusInternalServerError)
		return
	}
	// The safety scan can wait: queue it and answer. The worker scans the URL and removes the link if it is not safe.
	// The scan must not be lost, so if it cannot be queued, the link is not kept.
	if err := s.Events.Publish(r.Context(), "scan", "scan-"+code, map[string]string{"code": code, "url": in.URL}); err != nil {
		_ = s.Links.Delete(r.Context(), code)
		http.Error(w, "queue unavailable", http.StatusServiceUnavailable)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code})
}

// tooMany answers 429 with a Retry-After header when the client has used its tokens.
// If the limiter itself fails, the request goes through: a broken limiter must not take the service down.
func (s *Server) tooMany(w http.ResponseWriter, r *http.Request, client string) bool {
	if s.Limiter == nil {
		return false
	}
	ok, wait, err := s.Limiter.Allow(r.Context(), client)
	if err != nil || ok {
		return false
	}
	w.Header().Set("Retry-After", strconv.Itoa(max(1, int((wait+time.Second-1)/time.Second))))
	http.Error(w, "too many requests", http.StatusTooManyRequests)
	return true
}

func (s *Server) open(w http.ResponseWriter, r *http.Request) {
	opens.Add(1)
	code := r.PathValue("code")
	long, ok, err := s.Links.Lookup(r.Context(), code)
	if err != nil {
		http.Error(w, "lookup failed", http.StatusInternalServerError)
		return
	}
	if !ok {
		http.NotFound(w, r)
		return
	}
	// Analytics can wait: queue the click and answer. If the queue is down the click is lost, and the open still works.
	_ = s.Events.Publish(r.Context(), "click", rand.Text(), map[string]string{"code": code})
	// 302, not 301: a 301 is cached by the browser, so later opens would never reach us.
	http.Redirect(w, r, long, http.StatusFound)
}

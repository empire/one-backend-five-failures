// Package server holds the HTTP handlers of the URL shortener.
package server

import (
	"context"
	"encoding/json"
	"expvar"
	"net/http"
	"net/url"

	"shortener/internal/session"
	"shortener/internal/thirdparty"
)

// Links is where short links are saved and found: the database, or the database behind a cache.
type Links interface {
	Create(ctx context.Context, url string) (string, error)
	Lookup(ctx context.Context, code string) (url string, ok bool, err error)
}

var opens = expvar.NewInt("opens") // opens answered, shown at /debug/vars

// Server wires the handlers to their dependencies.
type Server struct {
	Sessions session.Store
	Links    Links
	Third    *thirdparty.Client
}

// Handler returns the routes:
//
//	POST /login      start a session (demo only: no password)
//	POST /links      create a short link (needs a session)
//	GET  /{code}     open a short link (302 to the long URL)
func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("POST /login", s.login)
	mux.HandleFunc("POST /links", s.create)
	mux.HandleFunc("GET /{code}", s.open)
	mux.Handle("GET /debug/vars", expvar.Handler())
	return mux
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
	_, ok, err := s.Sessions.User(r.Context(), c.Value)
	if err != nil {
		http.Error(w, "session store down", http.StatusServiceUnavailable)
		return
	}
	if !ok {
		http.Error(w, "unknown session", http.StatusUnauthorized)
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
	// Called on the spot: this request waits for the scan.
	safe, err := s.Third.Scan(r.Context(), in.URL)
	if err != nil {
		http.Error(w, "scan failed", http.StatusBadGateway)
		return
	}
	if !safe {
		http.Error(w, "URL is not safe", http.StatusUnprocessableEntity)
		return
	}
	code, err := s.Links.Create(r.Context(), in.URL)
	if err != nil {
		http.Error(w, "could not save", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(map[string]string{"code": code})
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
	// Called on the spot: this request waits for analytics, and ignores its errors.
	_ = s.Third.Click(r.Context(), code)
	// 302, not 301: a 301 is cached by the browser, so later opens would never reach us.
	http.Redirect(w, r, long, http.StatusFound)
}

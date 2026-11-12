// Package server is the companion's HTTP side: device pairing, the /hc
// WebSocket that bridges a phone to `hc connect`, and the PWA's static files.
package server

import (
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/coder/websocket"

	"herdr-anywhere/companion/internal/store"
)

type Config struct {
	Store *store.Store
	// HC is the hc binary to run; empty means none was found, and /hc answers 503.
	HC string
	// HCVersion is `hc --version`'s output; empty reports "hc": null.
	HCVersion string
	// Name is this computer's name, shown to the phone.
	Name    string
	Version string
	// WebDir holds the PWA; empty serves no files.
	WebDir string
	// Home is what "~" means in /api/folders and /api/image; empty is this user's home.
	Home string
	// ImageTemp are the temp folders /api/image reads pictures from besides home; nil is the system's.
	ImageTemp []string
	Logger    *log.Logger
	// Updates answers about new versions; nil means this companion doesn't know of any.
	Updates Updates
	// RevokePoll is how often live sockets are checked against the device
	// list (default 500 ms), so `revoke` from another process takes effect.
	RevokePoll time.Duration
}

type Server struct {
	cfg     Config
	log     *log.Logger
	limiter *limiter
	static  http.Handler

	mu       sync.Mutex
	sessions map[*session]struct{}
	closing  bool
	active   sync.WaitGroup // one per live session
	stop     chan struct{}
	stopOnce sync.Once

	maxUpload    int64
	machineChunk int
	maxImage     int64
	// The temp folders a picture on another machine may be in; nil: that machine's /tmp and $TMPDIR (tests set it).
	remoteTemp []string
}

func New(cfg Config) *Server {
	if cfg.Logger == nil {
		cfg.Logger = log.New(io.Discard, "", 0)
	}
	if cfg.RevokePoll <= 0 {
		cfg.RevokePoll = 500 * time.Millisecond
	}
	s := &Server{
		cfg:      cfg,
		log:      cfg.Logger,
		limiter:  newLimiter(10, time.Minute),
		static:   http.NotFoundHandler(),
		sessions: map[*session]struct{}{},
		stop:     make(chan struct{}),

		maxUpload:    maxUpload,
		machineChunk: machineChunk,
		maxImage:     maxImage,
	}
	if cfg.WebDir != "" {
		st, err := newStatic(cfg.WebDir)
		if err != nil {
			s.log.Printf("web dir: %v (serving no files)", err)
		} else {
			s.static = st
		}
	}
	go s.watchRevocations()
	return s
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()
	mux.HandleFunc("GET /api/info", s.info)
	mux.HandleFunc("/api/pair", s.pair)
	mux.HandleFunc("GET /api/folders", s.folders)
	mux.HandleFunc("POST /api/folders", s.createFolder)
	mux.HandleFunc("POST /api/files", s.upload)
	mux.HandleFunc("GET /api/image", s.image)
	mux.HandleFunc("POST /api/image/check", s.imageCheck)
	mux.HandleFunc("POST /api/update", s.update)
	mux.HandleFunc("POST /api/update/auto", s.updateAuto)
	mux.HandleFunc("POST /api/update/checks", s.updateChecks)
	mux.HandleFunc("POST /api/update/check", s.updateCheck)
	mux.HandleFunc("/hc", s.hc)
	mux.Handle("/", s.static)
	return s.logRequests(mux)
}

// Close stops the revocation watcher, ends every live socket and returns once
// their handlers are done (http.Server.Shutdown does not wait for hijacked
// connections).
func (s *Server) Close() {
	s.stopOnce.Do(func() { close(s.stop) })
	s.mu.Lock()
	s.closing = true
	live := make([]*session, 0, len(s.sessions))
	for sess := range s.sessions {
		live = append(live, sess)
	}
	s.mu.Unlock()
	var wg sync.WaitGroup
	for _, sess := range live {
		wg.Add(1)
		go func() {
			defer wg.Done()
			sess.finish(websocket.StatusGoingAway, "companion stopped")
		}()
	}
	wg.Wait()
	s.active.Wait()
}

// --- request log -------------------------------------------------------

type recorder struct {
	http.ResponseWriter
	status int
}

func (r *recorder) WriteHeader(code int) {
	if r.status == 0 {
		r.status = code
	}
	r.ResponseWriter.WriteHeader(code)
}

func (r *recorder) Write(b []byte) (int, error) {
	if r.status == 0 {
		r.status = http.StatusOK
	}
	return r.ResponseWriter.Write(b)
}

// Unwrap lets the WebSocket library reach the Hijacker underneath.
func (r *recorder) Unwrap() http.ResponseWriter { return r.ResponseWriter }

// logRequests logs method, path and status. Never the query: it carries the token.
func (s *Server) logRequests(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &recorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		s.log.Printf("%s %s %s %d %s", remoteIP(r), r.Method, r.URL.Path, rec.status, time.Since(start).Round(time.Millisecond))
	})
}

func remoteIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

// --- origin ------------------------------------------------------------

// sameOrigin allows requests without Origin (native apps) and requests whose
// Origin is exactly this server's own scheme://host (the PWA it serves).
func sameOrigin(r *http.Request) bool {
	origin := r.Header.Get("Origin")
	if origin == "" {
		return true
	}
	u, err := url.Parse(origin)
	if err != nil || u.Host == "" || (u.Path != "" && u.Path != "/") || u.RawQuery != "" || u.User != nil {
		return false
	}
	scheme := "http"
	if r.TLS != nil {
		scheme = "https"
	}
	return strings.EqualFold(u.Scheme, scheme) && strings.EqualFold(u.Host, r.Host)
}

// --- /api/info -----------------------------------------------------------

func writeJSON(w http.ResponseWriter, code int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(code)
	_ = json.NewEncoder(w).Encode(v)
}

func (s *Server) info(w http.ResponseWriter, r *http.Request) {
	var hc *string
	if s.cfg.HCVersion != "" {
		hc = &s.cfg.HCVersion
	}
	info := map[string]any{"name": s.cfg.Name, "version": s.cfg.Version, "hc": hc}
	if u := s.cfg.Updates; u != nil {
		info["latest"] = u.Latest()
		if a := u.App(); a.URL != "" {
			info["app"] = a
		}
		info["canUpdate"] = u.CanInstall()
		info["autoUpdate"] = u.Auto()
		info["checks"] = u.Checks()
	}
	writeJSON(w, http.StatusOK, info)
}

// --- /api/pair -----------------------------------------------------------

func (s *Server) pair(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		w.Header().Set("Allow", "POST")
		writeJSON(w, http.StatusMethodNotAllowed, map[string]string{"error": "POST only"})
		return
	}
	if !sameOrigin(r) {
		writeJSON(w, http.StatusForbidden, map[string]string{"error": "foreign origin"})
		return
	}
	ip := remoteIP(r)
	if !s.limiter.allow(ip) {
		writeJSON(w, http.StatusTooManyRequests, map[string]string{"error": "too many failed pairing attempts; wait a minute"})
		return
	}
	var body struct {
		Code   string `json:"code"`
		Device string `json:"device"`
	}
	if err := json.NewDecoder(io.LimitReader(r.Body, 4096)).Decode(&body); err != nil {
		writeJSON(w, http.StatusBadRequest, map[string]string{"error": "body must be {\"code\":…,\"device\":…}"})
		return
	}
	name := strings.TrimSpace(body.Device)
	if len(name) > 100 {
		name = name[:100]
	}
	d, token, err := s.cfg.Store.Redeem(body.Code, name)
	switch {
	case errors.Is(err, store.ErrBadCode):
		s.limiter.fail(ip)
		writeJSON(w, http.StatusUnauthorized, map[string]string{"error": "unknown pairing code"})
	case errors.Is(err, store.ErrCodeGone):
		s.limiter.fail(ip)
		writeJSON(w, http.StatusGone, map[string]string{"error": "pairing code expired or already used; run herdr-anywhere pair again"})
	case err != nil:
		s.log.Printf("pair: %v", err)
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": "could not store the pairing"})
	default:
		s.log.Printf("paired device %s (%q) from %s", d.ID, d.Name, ip)
		writeJSON(w, http.StatusOK, map[string]string{"token": token, "device": d.ID, "name": s.cfg.Name})
	}
}

// --- rate limit ----------------------------------------------------------

// limiter blocks an IP after n failures within window.
type limiter struct {
	n      int
	window time.Duration
	now    func() time.Time

	mu       sync.Mutex
	failures map[string][]time.Time
}

func newLimiter(n int, window time.Duration) *limiter {
	return &limiter{n: n, window: window, now: time.Now, failures: map[string][]time.Time{}}
}

func (l *limiter) recent(ip string) []time.Time {
	cut := l.now().Add(-l.window)
	kept := l.failures[ip][:0]
	for _, t := range l.failures[ip] {
		if t.After(cut) {
			kept = append(kept, t)
		}
	}
	if len(kept) == 0 {
		delete(l.failures, ip)
		return nil
	}
	l.failures[ip] = kept
	return kept
}

func (l *limiter) allow(ip string) bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.recent(ip)) < l.n
}

func (l *limiter) fail(ip string) {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.failures[ip] = append(l.recent(ip), l.now())
}

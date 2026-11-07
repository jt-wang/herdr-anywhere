package server

import (
	"bytes"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/store"
	"herdr-anywhere/companion/internal/testfake"
)

// syncBuffer is a log sink tests can read while the server writes.
type syncBuffer struct {
	mu sync.Mutex
	b  bytes.Buffer
}

func (s *syncBuffer) Write(p []byte) (int, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.Write(p)
}

func (s *syncBuffer) String() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.b.String()
}

type fixture struct {
	srv   *httptest.Server
	s     *Server
	store *store.Store
	logs  *syncBuffer
}

// fakeHC is a stand-in for hc: `--version` answers like hc, `connect`
// echoes, recording its pid in $HM_PIDFILE when set.
func fakeHC(t *testing.T) string {
	t.Helper()
	return testfake.HC(t, t.TempDir(), "hc", testfake.Spec{Version: "hc 9.9.9", Connect: "echo"})
}

func newFixture(t *testing.T, mod func(*Config)) *fixture {
	t.Helper()
	st, err := store.Open(filepath.Join(t.TempDir(), "state"))
	if err != nil {
		t.Fatal(err)
	}
	logs := &syncBuffer{}
	cfg := Config{
		Store:      st,
		Name:       "test-mac",
		Version:    "0.0.0-test",
		Logger:     log.New(logs, "", 0),
		RevokePoll: 100 * time.Millisecond,
	}
	if mod != nil {
		mod(&cfg)
	}
	s := New(cfg)
	srv := httptest.NewServer(s.Handler())
	t.Cleanup(func() {
		srv.Close()
		s.Close()
	})
	return &fixture{srv: srv, s: s, store: st, logs: logs}
}

// pair makes a device through the store and returns its id and token.
func (f *fixture) pair(t *testing.T) (string, string) {
	t.Helper()
	code, err := f.store.CreateCode("Phone", 10*time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	d, token, err := f.store.Redeem(code, "test phone")
	if err != nil {
		t.Fatal(err)
	}
	return d.ID, token
}

func get(t *testing.T, url string) (*http.Response, string) {
	t.Helper()
	r, err := http.Get(url)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return r, string(b)
}

func post(t *testing.T, url, body string, header ...string) (*http.Response, string) {
	t.Helper()
	req, _ := http.NewRequest("POST", url, strings.NewReader(body))
	req.Header.Set("Content-Type", "application/json")
	for i := 0; i+1 < len(header); i += 2 {
		req.Header.Set(header[i], header[i+1])
	}
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	return r, string(b)
}

package main

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"
)

// companion stands in for the Companion: /api/chat/t1 answers with the turn's end the test sets, numbered.
func companion(t *testing.T, end *atomic.Value) (*httptest.Server, *atomic.Int32) {
	var n atomic.Int32
	s := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		k := n.Add(1)
		switch r.URL.Path {
		case "/api/chat/t1":
			fmt.Fprintf(w, `{"chat":true,"turns":[{"id":"u%d","end":%q}]}`, k, end.Load().(string))
		case "/api/chat/t1/steps/s1":
			fmt.Fprint(w, `{"input":"in","output":"out"}`)
		default:
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(s.Close)
	return s, &n
}

func proxied(t *testing.T, to string, ctl string) *httptest.Server {
	u, err := url.Parse(to)
	if err != nil {
		t.Fatal(err)
	}
	p := httptest.NewServer(New(u, ctl))
	t.Cleanup(p.Close)
	return p
}

func get(t *testing.T, u string) map[string]any {
	t.Helper()
	r, err := http.Get(u)
	if err != nil {
		t.Fatal(err)
	}
	defer r.Body.Close()
	b, _ := io.ReadAll(r.Body)
	var m map[string]any
	if err := json.Unmarshal(b, &m); err != nil {
		t.Fatalf("%s: %s: %v", u, b, err)
	}
	return m
}

func touch(t *testing.T, path, s string) {
	t.Helper()
	if err := os.WriteFile(path, []byte(s), 0o644); err != nil {
		t.Fatal(err)
	}
}

func exists(path string) bool { _, err := os.Stat(path); return err == nil }

func waitFile(t *testing.T, path string) {
	t.Helper()
	for end := time.Now().Add(5 * time.Second); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if exists(path) {
			return
		}
	}
	t.Fatalf("%s never appeared", filepath.Base(path))
}

func TestPassesThrough(t *testing.T) {
	var end atomic.Value
	end.Store("done")
	c, _ := companion(t, &end)
	p := proxied(t, c.URL, t.TempDir())
	if m := get(t, p.URL+"/api/chat/t1"); m["chat"] != true || m["unknown"] != nil {
		t.Fatalf("chat: %v", m)
	}
	if m := get(t, p.URL+"/api/chat/t1/steps/s1"); m["input"] != "in" {
		t.Fatalf("step: %v", m)
	}
}

// no-status: the next chat answer only says the computer can't tell, once; a step's answer is not a chat answer.
func TestNoStatusOnce(t *testing.T) {
	var end atomic.Value
	end.Store("done")
	c, _ := companion(t, &end)
	ctl := t.TempDir()
	p := proxied(t, c.URL, ctl)
	touch(t, filepath.Join(ctl, "no-status"), "")
	if m := get(t, p.URL+"/api/chat/t1/steps/s1"); m["input"] != "in" {
		t.Fatalf("step: %v", m)
	}
	if m := get(t, p.URL+"/api/chat/t1"); m["unknown"] != "no-status" || m["turns"] != nil {
		t.Fatalf("first chat answer after no-status: %v", m)
	}
	if !exists(filepath.Join(ctl, "no-status-sent")) || exists(filepath.Join(ctl, "no-status")) {
		t.Fatal("no-status was not turned into no-status-sent")
	}
	if m := get(t, p.URL+"/api/chat/t1"); m["unknown"] != nil || m["turns"] == nil {
		t.Fatalf("the answer after: %v", m)
	}
}

// hold: the first chat answer whose last turn is still open is kept until release; answers after it pass, newer.
func TestHoldUntilRelease(t *testing.T) {
	var end atomic.Value
	end.Store("done")
	c, _ := companion(t, &end)
	ctl := t.TempDir()
	p := proxied(t, c.URL, ctl)
	touch(t, filepath.Join(ctl, "hold"), "")
	// A finished turn is not what is held.
	if m := get(t, p.URL+"/api/chat/t1"); m["turns"] == nil {
		t.Fatalf("done answer: %v", m)
	}
	end.Store("open")
	held := make(chan map[string]any, 1)
	go func() { held <- get(t, p.URL+"/api/chat/t1") }()
	waitFile(t, filepath.Join(ctl, "held"))
	end.Store("done")
	newer := get(t, p.URL+"/api/chat/t1")
	if tl := newer["turns"].([]any); tl[0].(map[string]any)["end"] != "done" {
		t.Fatalf("newer: %v", newer)
	}
	select {
	case m := <-held:
		t.Fatalf("the held answer came before release: %v", m)
	case <-time.After(300 * time.Millisecond):
	}
	touch(t, filepath.Join(ctl, "release"), "")
	select {
	case m := <-held:
		if m["turns"].([]any)[0].(map[string]any)["end"] != "open" {
			t.Fatalf("held answer: %v", m)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the held answer never came after release")
	}
	waitFile(t, filepath.Join(ctl, "released"))
	if exists(filepath.Join(ctl, "hold")) || exists(filepath.Join(ctl, "release")) {
		t.Fatal("hold or release left behind")
	}
}

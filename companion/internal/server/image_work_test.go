package server

import (
	"context"
	"encoding/json"
	"errors"
	"herdr-anywhere/companion/internal/testfake"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func imageWorkReason(err error) string {
	var e *imageError
	if errors.As(err, &e) {
		return e.why
	}
	return ""
}

func TestImageWorkDeadlineKeepsSlotUntilReadFinishes(t *testing.T) {
	slots := make(chan struct{}, 2)
	release := make(chan struct{})
	defer close(release)
	entered := make(chan struct{}, 2)
	var calls atomic.Int32
	work := func() (int, error) { calls.Add(1); entered <- struct{}{}; <-release; return 7, nil }
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
		defer cancel()
		done := make(chan error, 1)
		go func() { _, err := runImageTask(ctx, slots, work); done <- err }()
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("worker did not start")
		}
		select {
		case err := <-done:
			if imageWorkReason(err) != "timeout" {
				t.Fatalf("deadline reason: %v", err)
			}
		case <-time.After(200 * time.Millisecond):
			t.Fatal("blocked filesystem read held the HTTP task past its deadline")
		}
	}
	for i := 0; i < 20; i++ {
		_, err := runImageTask(context.Background(), slots, func() (int, error) { calls.Add(1); return 8, nil })
		if imageWorkReason(err) != "busy" {
			t.Fatalf("request %d bypassed occupied slots: %v", i, err)
		}
	}
	if calls.Load() != 2 {
		t.Fatalf("started %d workers; want only the two blocked reads", calls.Load())
	}
}

func TestImageWorkCancellationAndIndependentCapacity(t *testing.T) {
	reads, checks := make(chan struct{}, 2), make(chan struct{}, 2)
	release := make(chan struct{})
	defer close(release)
	entered := make(chan struct{})
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() {
		_, err := runImageTask(ctx, checks, func() (int, error) { close(entered); <-release; return 0, nil })
		done <- err
	}()
	<-entered
	cancel()
	select {
	case err := <-done:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancel: %v", err)
		}
	case <-time.After(200 * time.Millisecond):
		t.Fatal("cancel did not return")
	}
	if len(checks) != 1 {
		t.Fatal("cancel released a worker that is still blocked")
	}
	v, err := runImageTask(context.Background(), reads, func() (int, error) { return 9, nil })
	if err != nil || v != 9 {
		t.Fatalf("read starved behind checks: %d %v", v, err)
	}
	var called bool
	_, err = runImageTask(ctx, reads, func() (int, error) { called = true; return 0, nil })
	if !errors.Is(err, context.Canceled) || called {
		t.Fatal("already cancelled request started work")
	}
}

func TestImageWorkReleasesCompletedSlotAndPreservesErrors(t *testing.T) {
	slots := make(chan struct{}, 1)
	want := refuse(http.StatusForbidden, "outside", "outside")
	_, err := runImageTask(context.Background(), slots, func() (int, error) { return 0, want })
	if err != want {
		t.Fatalf("changed path error: %v", err)
	}
	if len(slots) != 0 {
		t.Fatal("completed worker retained slot")
	}
	v, err := runImageTask(context.Background(), slots, func() (int, error) { return 5, nil })
	if err != nil || v != 5 {
		t.Fatalf("next request: %d %v", v, err)
	}
}

func TestImageHandlersHonorDeadlineAndKeepSeparateSlots(t *testing.T) {
	hc := testfake.HC(t, t.TempDir(), "hc", testfake.Spec{On: map[string][]testfake.Reply{"get": {{Sleep: "3s"}}}})
	f := newFixture(t, func(c *Config) { c.HC = hc; c.Home = t.TempDir() })
	_, token := f.pair(t)
	for _, kind := range []string{"read", "check"} {
		t.Run(kind, func(t *testing.T) {
			method, path, body := "GET", "/api/image?path=shot.png&terminal=t1", ""
			if kind == "check" {
				method, path, body = "POST", "/api/image/check", `{"terminal":"t1","paths":["shot.png"]}`
			}
			ctx, cancel := context.WithTimeout(context.Background(), 40*time.Millisecond)
			defer cancel()
			r := httptest.NewRequest(method, path, strings.NewReader(body)).WithContext(ctx)
			r.Header.Set("Authorization", "Bearer "+token)
			w := httptest.NewRecorder()
			start := time.Now()
			f.s.Handler().ServeHTTP(w, r)
			if w.Code != http.StatusGatewayTimeout {
				t.Fatalf("status %d, want deadline", w.Code)
			}
			var response map[string]any
			json.Unmarshal(w.Body.Bytes(), &response)
			if response["why"] != "timeout" {
				t.Fatalf("reason: %v", response["why"])
			}
			if time.Since(start) > 500*time.Millisecond {
				t.Fatal("request deadline ignored")
			}
		})
	}
	// The request context must stop the slow hc child, not just abandon its handler.
	deadline := time.Now().Add(time.Second)
	for len(f.s.imageReads)+len(f.s.imageChecks) > 0 && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if len(f.s.imageReads)+len(f.s.imageChecks) > 0 {
		t.Fatal("request timeout left hc worker alive")
	}
	// A saturated background-check pool cannot consume the foreground-read pool.
	for i := 0; i < cap(f.s.imageChecks); i++ {
		f.s.imageChecks <- struct{}{}
	}
	defer func() {
		for len(f.s.imageChecks) > 0 {
			<-f.s.imageChecks
		}
	}()
	r := httptest.NewRequest("POST", "/api/image/check", strings.NewReader(`{"paths":["shot.png"]}`))
	r.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	f.s.Handler().ServeHTTP(w, r)
	if w.Code != http.StatusServiceUnavailable || !strings.Contains(w.Body.String(), `"why":"busy"`) {
		t.Fatalf("busy check: %d %s", w.Code, w.Body.String())
	}
	if len(f.s.imageReads) != 0 {
		t.Fatal("background checks occupied read slots")
	}
}

func TestImageCancelledBatchDoesNotStartFileWork(t *testing.T) {
	s := New(Config{Home: t.TempDir()})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := s.checkHere(ctx, "", []string{"shot.png"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled batch: %v", err)
	}
}

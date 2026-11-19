package main

import (
	"context"
	"encoding/json"
	"net/http"
	"strings"
	"sync"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/tailcat"
)

// fakeWindow is a serve's window as the control socket shows it: a phone asks once asked() runs, and the answer
// decides how the window ends, as connecthost does (a no or a phone that leaves spends the code).
type fakeWindow struct {
	mu       sync.Mutex
	st       tailcat.Status
	answered []bool
}

func (f *fakeWindow) Status(id string) (tailcat.Status, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id != "w1" {
		return tailcat.Status{}, tailcat.ErrNoWindow
	}
	return f.st, nil
}

func (f *fakeWindow) Answer(id string, approve bool) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	if id != "w1" {
		return tailcat.ErrNoWindow
	}
	if f.st.State != tailcat.StateAsking {
		return tailcat.ErrNotAsking
	}
	f.answered = append(f.answered, approve)
	if approve {
		f.st = tailcat.Status{State: tailcat.StatePaired, Device: f.st.Request.Device}
	} else {
		f.st = tailcat.Status{State: tailcat.StateEnded}
	}
	return nil
}

func (f *fakeWindow) ask() {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.st = tailcat.Status{State: tailcat.StateAsking, Request: &tailcat.Request{Device: "Pixel 9", Emoji: []string{"🐶", "🌵", "🔑", "🚲"}, Route: "lan"}}
}

func fakeConfirmServe(t *testing.T, dir string) *fakeWindow {
	t.Helper()
	f := &fakeWindow{st: tailcat.Status{State: tailcat.StateWaiting}}
	ln, err := tailcat.Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	open := func(context.Context, string) (tailcat.Window, error) {
		return tailcat.Window{Invite: tailcat.Prefix2 + "INVITE", ID: "w1", Expires: time.Now().Add(2 * time.Minute)}, nil
	}
	srv := &http.Server{Handler: tailcat.ControlHandler(open, f, nil)}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	return f
}

func withPairInput(t *testing.T, text string) {
	t.Helper()
	in, poll := pairInput, pairPoll
	t.Cleanup(func() { pairInput, pairPoll = in, poll })
	pairInput, pairPoll = strings.NewReader(text), 10*time.Millisecond
}

// pair shows the phone's name and emoji and lets it pair only on a y; anything else (Enter alone) is a no.
func TestPairAsksAboutTheEmojiAndPairsOnlyOnYes(t *testing.T) {
	for _, c := range []struct {
		input string
		code  int
		says  string
	}{
		{"y\n", 0, "Pixel 9 is paired."},
		{"\n", 1, "Not paired. This code no longer works."},
		{"", 1, "Not paired."}, // no terminal to answer from
	} {
		dir := shortStateDir(t)
		f := fakeConfirmServe(t, dir)
		withPairInput(t, c.input)
		time.AfterFunc(100*time.Millisecond, f.ask)
		out, errs, code := runCLI(t, "pair", "--state", dir)
		if code != c.code || !strings.Contains(out, c.says) {
			t.Fatalf("input %q: exit %d, want %d and %q:\n%s\n%s", c.input, code, c.code, c.says, out, errs)
		}
		if !strings.Contains(out, "herdr2:INVITE") || !strings.Contains(out, "Pixel 9 wants to pair") || !strings.Contains(out, "🐶  🌵  🔑  🚲") {
			t.Fatalf("input %q: the invite, the name or the emoji missing:\n%s", c.input, out)
		}
		if want := c.code == 0; len(f.answered) != 1 || f.answered[0] != want {
			t.Fatalf("input %q: answers %v", c.input, f.answered)
		}
	}
}

// The Add phone dialog follows the window and answers through pair --json --window.
func TestPairJSONWindowFollowsAndAnswers(t *testing.T) {
	dir := shortStateDir(t)
	f := fakeConfirmServe(t, dir)
	out, _, code := runCLI(t, "pair", "--json", "--state", dir)
	var win struct{ Invite, ID string }
	if code != 0 || json.Unmarshal([]byte(out), &win) != nil || win.ID != "w1" || win.Invite != tailcat.Prefix2+"INVITE" {
		t.Fatalf("exit %d: %s", code, out)
	}
	status := func(args ...string) (tailcat.Status, string, int) {
		out, _, code := runCLI(t, append([]string{"pair", "--json", "--window", "w1", "--state", dir}, args...)...)
		var st tailcat.Status
		json.Unmarshal([]byte(out), &st)
		return st, out, code
	}
	if st, out, _ := status(); st.State != tailcat.StateWaiting {
		t.Fatalf("before the phone: %s", out)
	}
	if _, out, code := status("--answer", "yes"); code != 1 || !strings.Contains(out, `"not-asking"`) {
		t.Fatalf("a yes with no phone asking: exit %d %s", code, out)
	}
	f.ask()
	if st, out, _ := status(); st.State != tailcat.StateAsking || st.Request.Device != "Pixel 9" || len(st.Request.Emoji) != 4 {
		t.Fatalf("asking: %s", out)
	}
	if st, out, code := status("--answer", "yes"); code != 0 || st.State != tailcat.StatePaired {
		t.Fatalf("yes: exit %d %s", code, out)
	}
	if out, _, code := runCLI(t, "pair", "--json", "--window", "nope", "--state", dir); code != 1 || !strings.Contains(out, `"no-window"`) {
		t.Fatalf("unknown window: exit %d %s", code, out)
	}
	for _, args := range [][]string{{"--window", "w1"}, {"--json", "--answer", "yes"}, {"--json", "--window", "w1", "--answer", "maybe"}} {
		if _, _, code := runCLI(t, append([]string{"pair", "--state", dir}, args...)...); code != 2 {
			t.Errorf("%v: exit %d, want 2", args, code)
		}
	}
}

// --no-lan and --tailcat-only go with --tailcat, and not with the routes they turn off.
func TestRouteFlagsRefuseWhatContradicts(t *testing.T) {
	for _, args := range [][]string{
		{"--no-lan"},
		{"--tailcat-only"},
		{"--tailcat", "--tailcat-only", "--lan"},
		{"--tailcat", "--no-lan", "--lan"},
		{"--tailcat", "--tailcat-only", "--relay", "wss://relay.example.com", "--relay-token-file", "/x"},
	} {
		_, errs, code := runCLI(t, append([]string{"serve", "--state", shortStateDir(t)}, args...)...)
		if code != 2 || !strings.Contains(errs, "--") {
			t.Errorf("serve %v: exit %d: %s", args, code, errs)
		}
	}
	for _, args := range [][]string{{"--tailcat-only", "--lan"}, {"--no-lan", "--lan"}} {
		if _, errs, code := runCLI(t, append([]string{"start", "--state", shortStateDir(t)}, args...)...); code != 2 {
			t.Errorf("start %v: exit %d: %s", args, code, errs)
		}
	}
}

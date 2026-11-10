package main

import (
	"net/netip"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/autostart"
)

// withLAN gives start these LAN addresses, as the computer's network would.
func withLAN(t *testing.T, addrs ...string) {
	t.Helper()
	restore := lanAddresses
	t.Cleanup(func() { lanAddresses = restore })
	lanAddresses = func() ([]netip.Addr, error) {
		var out []netip.Addr
		for _, a := range addrs {
			out = append(out, netip.MustParseAddr(a))
		}
		return out, nil
	}
}

// What serve start runs, from what this start was given. --lan keeps serve's own port, 7681: a LAN phone keeps the
// address it paired with, so a port that changes at each login would lose it.
func TestStartedServeArgs(t *testing.T) {
	cases := []struct {
		name string
		o    serveOpts
		want string
	}{
		{"plain", serveOpts{}, "serve --tailcat --listen 127.0.0.1:0 --state /s"},
		{"lan", serveOpts{lan: true}, "serve --tailcat --lan --state /s"},
		{"listen", serveOpts{listen: []string{"100.64.0.5:7681", "127.0.0.1:7681"}}, "serve --tailcat --listen 100.64.0.5:7681 --listen 127.0.0.1:7681 --state /s"},
		{"lan and listen", serveOpts{lan: true, listen: []string{"127.0.0.1:9000"}}, "serve --tailcat --listen 127.0.0.1:9000 --lan --state /s"},
	}
	for _, c := range cases {
		if got := strings.Join(startedServeArgs("/s", c.o), " "); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

// The login entry on each system carries start's --lan and --listen; Windows adds the log its Run key needs.
func TestLoginEntryCarriesStartsOptions(t *testing.T) {
	o := serveOpts{lan: true, listen: []string{"100.64.0.5:7681"}}
	base := []string{"serve", "--tailcat", "--listen", "100.64.0.5:7681", "--lan", "--state", "/s"}
	for goos, want := range map[string][]string{
		"darwin":  base,
		"linux":   base,
		"windows": append(slices.Clone(base), "--log", filepath.Join("/s", serveLogFile)),
	} {
		e, err := loginEntryFor(goos, "/s", o)
		if err != nil {
			t.Fatal(err)
		}
		if !slices.Equal(e.Args, want) {
			t.Errorf("%s: %q, want %q", goos, e.Args, want)
		}
	}
}

// start --lan writes the login entry with --lan and runs that serve now; a later plain start writes it without:
// only what this start was given goes in.
func TestStartLanGoesIntoTheLoginEntry(t *testing.T) {
	withLAN(t, "192.168.1.5")
	dir := shortStateDir(t)
	f := &fakeLogin{onStart: func() { time.AfterFunc(100*time.Millisecond, func() { fakeTailcatServe(t, dir, nil) }) }}
	useLogin(t, f)
	noSpawn(t)
	out, errs, code := runCLI(t, "start", "--lan", "--state", dir)
	if code != 0 || len(f.installed) != 1 {
		t.Fatalf("exit %d, installed %d: %s %s", code, len(f.installed), out, errs)
	}
	if !slices.Contains(f.installed[0].Args, "--lan") || slices.Contains(f.installed[0].Args, "127.0.0.1:0") {
		t.Fatalf("entry %q", f.installed[0].Args)
	}
	if !strings.Contains(out, "started") || !strings.Contains(out, "you can close this terminal") {
		t.Fatalf("out %q", out)
	}
	runCLI(t, "start", "--state", dir)
	if slices.Contains(f.installed[len(f.installed)-1].Args, "--lan") {
		t.Fatalf("a plain start kept --lan: %q", f.installed[len(f.installed)-1].Args)
	}
}

// Where nothing starts the entry now (Windows), start runs the same serve itself.
func TestStartLanRunsThatServeWhereNoManagerStartsIt(t *testing.T) {
	withLAN(t, "192.168.1.5")
	dir := shortStateDir(t)
	useLogin(t, &fakeLogin{startErr: autostart.ErrNoManager})
	restore := spawnServe
	t.Cleanup(func() { spawnServe = restore })
	var ran []string
	spawnServe = func(args []string, _ *os.File) (<-chan error, func(), error) {
		ran = args
		time.AfterFunc(100*time.Millisecond, func() { fakeTailcatServe(t, dir, nil) })
		return make(chan error), func() {}, nil
	}
	if _, errs, code := runCLI(t, "start", "--lan", "--listen", "127.0.0.1:9000", "--state", dir); code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if want := []string{"serve", "--tailcat", "--listen", "127.0.0.1:9000", "--lan", "--state", dir}; !slices.Equal(ran, want) {
		t.Fatalf("ran %q", ran)
	}
}

// A serve already running keeps how it was started: start says the new options apply from the next login, and how to
// have them now.
func TestStartLanWithAServeRunningSaysWhenItApplies(t *testing.T) {
	withLAN(t, "192.168.1.5")
	dir := shortStateDir(t)
	fakeTailcatServe(t, dir, nil)
	f := &fakeLogin{}
	useLogin(t, f)
	noSpawn(t)
	runCLI(t, "start", "--state", dir) // the entry as a plain start wrote it
	out, errs, code := runCLI(t, "start", "--lan", "--state", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if !strings.Contains(out, "next login") || !strings.Contains(out, "herdr-anywhere stop") || !strings.Contains(out, "herdr-anywhere start --lan") {
		t.Fatalf("out %q", out)
	}
}

// What would only fail later, in the background, fails now: --lan with no LAN address, a --listen serve refuses.
func TestStartRefusesWhatServeWouldRefuse(t *testing.T) {
	dir := shortStateDir(t)
	useLogin(t, &fakeLogin{})
	noSpawn(t)
	withLAN(t)
	if _, errs, code := runCLI(t, "start", "--lan", "--state", dir); code == 0 || !strings.Contains(errs, "LAN") {
		t.Fatalf("--lan with no LAN address: exit %d: %s", code, errs)
	}
	withLAN(t, "192.168.1.5")
	if _, errs, code := runCLI(t, "start", "--listen", "0.0.0.0:7681", "--state", dir); code != 2 || !strings.Contains(errs, "0.0.0.0") {
		t.Fatalf("--listen 0.0.0.0: exit %d: %s", code, errs)
	}
}

// serve in a terminal names the start that keeps the same serve running: its --lan, and the --listen addresses a phone
// could use (start listens on this computer itself already).
func TestForegroundTipNamesTheSameStart(t *testing.T) {
	for _, c := range []struct {
		lan    bool
		listen []string
		want   string
	}{
		{false, nil, "herdr-anywhere start\n"},
		{true, nil, "herdr-anywhere start --lan\n"},
		{false, []string{"127.0.0.1:7681"}, "herdr-anywhere start\n"},
		{false, []string{"127.0.0.1:7681", "100.64.0.5:7681"}, "herdr-anywhere start --listen 100.64.0.5:7681\n"},
	} {
		got := foregroundTip(c.lan, c.listen)
		if !strings.HasSuffix(got+"\n", ": "+c.want) || !strings.Contains(got, "closes") {
			t.Errorf("lan %v listen %v: %q, want it to end with %q", c.lan, c.listen, got, c.want)
		}
	}
}

// pair's list of ways to start a serve the phone can reach names start for each, which keeps it after a restart.
func TestPairHintNamesStartForEachWay(t *testing.T) {
	for _, w := range []string{"herdr-anywhere start --lan", "herdr-anywhere start --listen <VPN address>:7681"} {
		if !strings.Contains(serveWays, w) {
			t.Errorf("want %q in:\n%s", w, serveWays)
		}
	}
}

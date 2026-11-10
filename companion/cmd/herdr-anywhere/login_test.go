package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/autostart"
	"herdr-anywhere/companion/internal/tailcat"
)

// No test reaches this user's real LaunchAgents, systemd units or Run key: each one that wants an entry sets a fake.
func init() { atLogin = func() loginStart { return &fakeLogin{installErr: autostart.ErrNotHere} } }

type fakeLogin struct {
	installed  []autostart.Entry
	started    int
	removed    int
	installErr error
	startErr   error
	onStart    func()
	has        bool
}

func (f *fakeLogin) Install(e autostart.Entry) (bool, error) {
	if f.installErr != nil {
		return false, f.installErr
	}
	changed := !f.has || (len(f.installed) > 0 && !slices.Equal(f.installed[len(f.installed)-1].Args, e.Args)) // the real ones rewrite a file that differs
	f.installed = append(f.installed, e)
	f.has = true
	return changed, nil
}

func (f *fakeLogin) Start() error {
	f.started++
	if f.startErr == nil && f.onStart != nil {
		f.onStart()
	}
	return f.startErr
}

func (f *fakeLogin) Remove() (bool, error) {
	f.removed++
	had := f.has
	f.has = false
	return had, nil
}

func useLogin(t *testing.T, f *fakeLogin) {
	t.Helper()
	restore := atLogin
	t.Cleanup(func() { atLogin = restore })
	atLogin = func() loginStart { return f }
}

// noSpawn fails the test when start runs serve itself.
func noSpawn(t *testing.T) {
	t.Helper()
	restore := spawnServe
	t.Cleanup(func() { spawnServe = restore })
	spawnServe = func([]string, *os.File) (<-chan error, func(), error) {
		t.Error("start ran serve itself; the service manager should have")
		return make(chan error), func() {}, nil
	}
}

func TestStartLeavesServeToTheServiceManager(t *testing.T) {
	dir := shortStateDir(t)
	f := &fakeLogin{onStart: func() { time.AfterFunc(200*time.Millisecond, func() { fakeTailcatServe(t, dir, nil) }) }}
	useLogin(t, f)
	noSpawn(t)
	out, errs, code := runCLI(t, "start", "--state", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if len(f.installed) != 1 || f.started != 1 {
		t.Fatalf("installed %d, started %d", len(f.installed), f.started)
	}
	e := f.installed[0]
	self, _ := os.Executable()
	wantArgs := startedServeArgs(dir, serveOpts{})
	if runtime.GOOS == "windows" {
		wantArgs = append(wantArgs, "--log", filepath.Join(dir, serveLogFile))
	}
	if e.Exe != self || !slices.Equal(e.Args, wantArgs) || e.Env["PATH"] != os.Getenv("PATH") || e.Log != filepath.Join(dir, serveLogFile) {
		t.Fatalf("entry %+v", e)
	}
	for _, w := range []string{"started", "when you log in", "herdr-anywhere stop"} {
		if !strings.Contains(out, w) {
			t.Errorf("want %q in %q", w, out)
		}
	}
	if !tailcat.ServeRunning(dir) {
		t.Fatal("start returned before serve answered")
	}
}

func TestStartWithServeRunningOnlySetsItToStartAtLogin(t *testing.T) {
	dir := shortStateDir(t)
	fakeTailcatServe(t, dir, nil)
	f := &fakeLogin{}
	useLogin(t, f)
	noSpawn(t)
	out, errs, code := runCLI(t, "start", "--state", dir)
	if code != 0 || f.started != 0 || len(f.installed) != 1 {
		t.Fatalf("exit %d, started %d, installed %d: %s", code, f.started, len(f.installed), errs)
	}
	if !strings.Contains(out, "already running") || !strings.Contains(out, "when you log in") {
		t.Fatalf("out %q", out)
	}
	out, _, _ = runCLI(t, "start", "--state", dir)
	if first, _, _ := strings.Cut(out, "\n"); first != "already running" { // then how to add a phone
		t.Fatalf("a second start: %q", out)
	}
}

func TestStartRunsServeItselfWhereNothingStartsItNow(t *testing.T) {
	for _, startErr := range []error{autostart.ErrNoManager, errors.New("launchctl bootstrap: Input/output error")} {
		dir := shortStateDir(t)
		f := &fakeLogin{startErr: startErr}
		useLogin(t, f)
		restore := spawnServe
		spawned := false
		spawnServe = func([]string, *os.File) (<-chan error, func(), error) {
			spawned = true
			time.AfterFunc(200*time.Millisecond, func() { fakeTailcatServe(t, dir, nil) })
			return make(chan error), func() {}, nil
		}
		out, errs, code := runCLI(t, "start", "--state", dir)
		spawnServe = restore
		if code != 0 || !spawned {
			t.Fatalf("%v: exit %d, spawned %v: %s", startErr, code, spawned, errs)
		}
		// Windows: the Run key starts it at login, and nothing can now. A manager that failed (the person turned it
		// off in Login Items, say) is said, and no start at login is promised.
		noManager := errors.Is(startErr, autostart.ErrNoManager)
		if strings.Contains(errs, "Input/output error") == noManager || strings.Contains(out, "when you log in") != noManager {
			t.Fatalf("%v: out %q, stderr %q", startErr, out, errs)
		}
	}
}

// What `stop` stops, in a process of its own: the test binary run as TestHelperServe.
func helperServe(t *testing.T) *exec.Cmd {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperServe$")
	cmd.Env = append(os.Environ(), "HM_HELPER_SERVE=1")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { cmd.Process.Kill() })
	return cmd
}

func TestHelperServe(t *testing.T) {
	if os.Getenv("HM_HELPER_SERVE") != "1" {
		t.Skip("run by helperServe")
	}
	time.Sleep(time.Minute)
}

func TestStopTakesServeOffLoginAndEndsIt(t *testing.T) {
	dir := shortStateDir(t)
	f := &fakeLogin{has: true}
	useLogin(t, f)
	child := helperServe(t)
	fakeTailcatServe(t, dir, nil)
	os.WriteFile(filepath.Join(dir, serveURLsFile), []byte(`{"urls":["http://127.0.0.1:1"],"pid":`+itoa(child.Process.Pid)+`}`), 0o600)
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()
	out, errs, code := runCLI(t, "stop", "--state", dir)
	if code != 0 || f.removed != 1 {
		t.Fatalf("exit %d, removed %d: %s", code, f.removed, errs)
	}
	select {
	case <-exited:
	case <-time.After(5 * time.Second):
		t.Fatal("serve is still running")
	}
	if !strings.Contains(out, "stopped") || !strings.Contains(out, "no longer starts when you log in") || !strings.Contains(out, "herdr-anywhere start") {
		t.Fatalf("out %q", out)
	}
}

func TestStopWithNothingRunning(t *testing.T) {
	dir := shortStateDir(t)
	useLogin(t, &fakeLogin{})
	out, errs, code := runCLI(t, "stop", "--state", dir)
	if code != 0 || !strings.Contains(out, "not running") {
		t.Fatalf("exit %d: %q %q", code, out, errs)
	}
}

func TestStopLeavesAProcessThatIsNotServe(t *testing.T) {
	// serve.json left behind by a serve that crashed, its pid since reused: nothing answers, so nothing is killed.
	dir := shortStateDir(t)
	useLogin(t, &fakeLogin{})
	child := helperServe(t)
	os.WriteFile(filepath.Join(dir, serveURLsFile), []byte(`{"urls":["http://127.0.0.1:1"],"pid":`+itoa(child.Process.Pid)+`}`), 0o600)
	exited := make(chan error, 1)
	go func() { exited <- child.Wait() }()
	runCLI(t, "stop", "--state", dir)
	select {
	case <-exited:
		t.Fatal("stop killed a process no serve answered for")
	case <-time.After(time.Second):
	}
}

func TestServeInATerminalSaysHowToKeepItRunning(t *testing.T) {
	restore := isTerminal
	t.Cleanup(func() { isTerminal = restore })
	for _, term := range []bool{true, false} {
		isTerminal = func(any) bool { return term }
		stdout, _ := startServe(t, "--listen", "127.0.0.1:0", "--state", shortStateDir(t))
		if got := strings.Contains(stdout.String(), "herdr-anywhere start"); got != term {
			t.Fatalf("terminal %v: %q", term, stdout)
		}
	}
}

func TestASecondTailcatServeStopsBeforeListening(t *testing.T) {
	dir := shortStateDir(t)
	fakeTailcatServe(t, dir, nil)
	var out, errb bytes.Buffer
	done := make(chan int, 1)
	go func() {
		done <- run([]string{"serve", "--tailcat", "--listen", "127.0.0.1:0", "--state", dir}, &out, &errb)
	}()
	select {
	case code := <-done:
		if code == 0 || strings.Contains(out.String(), "listening on") || !strings.Contains(errb.String(), "already running") {
			t.Fatalf("exit %d\nout %s\nerr %s", code, &out, &errb)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("a second serve kept going")
	}
}

func TestPairStartsCompanionWhenItIsNotRunning(t *testing.T) {
	dir := shortStateDir(t)
	f := &fakeLogin{onStart: func() { time.AfterFunc(200*time.Millisecond, func() { fakeTailcatServe(t, dir, nil) }) }}
	useLogin(t, f)
	noSpawn(t)
	out, errs, code := runCLI(t, "pair", "--state", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if f.started != 1 || !strings.Contains(out, tailcat.Prefix+"INVITE") {
		t.Fatalf("started %d:\n%s", f.started, out)
	}
	for _, not := range []string{"#pair=", "--lan"} {
		if strings.Contains(out, not) {
			t.Errorf("%q in:\n%s", not, out)
		}
	}
	if !strings.Contains(out, "wasn't running") {
		t.Errorf("does not say it started Companion:\n%s", out)
	}
}

func TestPairWithURLDoesNotStartCompanion(t *testing.T) {
	// --url names a way to reach serve of the person's own (a proxy, a tailnet name): theirs to start.
	f := &fakeLogin{}
	useLogin(t, f)
	noSpawn(t)
	if _, errs, code := runCLI(t, "pair", "--state", shortStateDir(t), "--url", "http://mac.example:7681"); code != 0 {
		t.Fatal(errs)
	}
	if f.started != 0 || len(f.installed) != 0 {
		t.Fatalf("started %d, installed %d", f.started, len(f.installed))
	}
}

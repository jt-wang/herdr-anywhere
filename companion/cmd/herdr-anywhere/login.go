package main

// Keeping serve running across restarts: start sets it to start at login (internal/autostart) and stop takes that
// away again.

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"time"

	"herdr-anywhere/companion/internal/autostart"
	"herdr-anywhere/companion/internal/store"
	"herdr-anywhere/companion/internal/tailcat"
)

// loginStart is the entry that starts serve at login; tests put a fake in its place.
type loginStart interface {
	Install(autostart.Entry) (bool, error)
	Start() error
	Remove() (bool, error)
}

var atLogin = func() loginStart { return autostart.Default() }

// isTerminal says whether w is a terminal someone is watching (serve's hint is for them, not for a log file).
var isTerminal = func(w any) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	fi, err := f.Stat()
	return err == nil && fi.Mode()&os.ModeCharDevice != 0
}

func itoa(i int) string { return strconv.Itoa(i) }

const (
	startsAtLogin = "it also starts by itself when you log in (`herdr-anywhere stop` stops it and turns that off)"
	canClose      = "It runs in the background: you can close this terminal."
)

// foregroundTip is for a serve run in a terminal: the start that keeps the same serve running. Loopback --listen
// addresses are left out: start listens on this computer itself already.
func foregroundTip(lan bool, listen []string) string {
	o := serveOpts{lan: lan}
	for _, l := range listen {
		host, _, err := net.SplitHostPort(l)
		if ip, perr := netip.ParseAddr(host); err == nil && perr == nil && ip.IsLoopback() {
			continue
		}
		o.listen = append(o.listen, l)
	}
	return "This serve stops when this terminal closes or you press Ctrl-C. To keep it running in the background, also after a restart: herdr-anywhere start" + o.flags()
}

// loginEntry is serve as start runs it, for the service manager: this file, at the path it was started from (an
// update replaces the file there), with this PATH so it finds herdr-core as the shell did.
func loginEntry(dir string, o serveOpts) (autostart.Entry, error) {
	return loginEntryFor(runtime.GOOS, dir, o)
}

// loginEntryFor is loginEntry on goos.
func loginEntryFor(goos, dir string, o serveOpts) (autostart.Entry, error) {
	self, err := os.Executable()
	if err != nil {
		return autostart.Entry{}, err
	}
	log := filepath.Join(dir, serveLogFile)
	args := startedServeArgs(dir, o)
	if goos == "windows" {
		args = append(args, "--log", log) // the Run key gives it no place for its output, and a console window
	}
	return autostart.Entry{Exe: self, Args: args, Env: map[string]string{"PATH": os.Getenv("PATH")}, Log: log}, nil
}

// startCompanion is start: serve set to start at login, and running now, by the service manager where there is one.
func startCompanion(st *store.Store, o serveOpts, stdout, stderr io.Writer) error {
	login := atLogin()
	e, err := loginEntry(st.Dir(), o)
	var changed bool
	if err == nil {
		changed, err = login.Install(e)
	}
	atLoginOK := err == nil
	if err != nil && !errors.Is(err, autostart.ErrNotHere) {
		fmt.Fprintf(stderr, "herdr-anywhere: could not set it to start when you log in: %v\n", err)
	}
	if tailcat.ServeRunning(st.Dir()) {
		if atLoginOK && changed && o.flags() != "" {
			// The serve running was started some other way; it keeps that until it starts again.
			fmt.Fprintf(stdout, "already running as it was started; with%s from the next login. To have that now: herdr-anywhere stop, then herdr-anywhere start%s\n", o.flags(), o.flags())
		} else if atLoginOK && changed {
			fmt.Fprintln(stdout, "already running; from now on "+startsAtLogin)
		} else {
			fmt.Fprintln(stdout, "already running")
		}
		return nil
	}
	logPath := filepath.Join(st.Dir(), serveLogFile)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	if atLoginOK {
		err := login.Start()
		if err == nil {
			if err := waitServe(st.Dir(), logPath, nil, func() {}); err != nil {
				return err
			}
			fmt.Fprintln(stdout, "started; "+startsAtLogin+". "+canClose)
			return nil
		}
		if !errors.Is(err, autostart.ErrNoManager) {
			fmt.Fprintf(stderr, "herdr-anywhere: the service manager did not start it (%v); starting it for this session only\n", err)
			atLoginOK = false
		}
	}
	exited, stop, err := spawnServe(startedServeArgs(st.Dir(), o), logFile)
	if err != nil {
		return err
	}
	if err := waitServe(st.Dir(), logPath, exited, stop); err != nil {
		return err
	}
	if atLoginOK {
		fmt.Fprintln(stdout, "started; "+startsAtLogin+". "+canClose)
	} else {
		fmt.Fprintln(stdout, "started. "+canClose)
	}
	return nil
}

// waitServe waits for serve to answer on dir. exited (nil under a service manager, which starts it again) is its
// end; stop ends a serve that never answers.
func waitServe(dir, logPath string, exited <-chan error, stop func()) error {
	deadline := time.After(startWait)
	tick := time.NewTicker(100 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-exited:
			if line := lastLine(logPath); line != "" {
				return errors.New(line)
			}
			return fmt.Errorf("serve stopped: %v", err)
		case <-deadline:
			stop()
			if line := lastLine(logPath); line != "" && exited == nil {
				return fmt.Errorf("serve did not answer within %s: %s (see %s)", startWait, line, logPath)
			}
			return fmt.Errorf("serve did not answer within %s; see %s", startWait, logPath)
		case <-tick.C:
			if tailcat.ServeRunning(dir) {
				return nil
			}
		}
	}
}

// --- stop ------------------------------------------------------------------

func stop(args []string, stdout, stderr io.Writer) error {
	fs, state := newFlags("stop", stderr)
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return usageError{fmt.Errorf("unexpected argument %q", fs.Arg(0))}
	}
	st, err := openStore(*state)
	if err != nil {
		return err
	}
	removed, err := atLogin().Remove()
	if err != nil {
		return fmt.Errorf("taking it off login: %w", err)
	}
	// The service manager stopped its own serve; one started otherwise (serve in a terminal, start without a
	// manager) is the pid it recorded, if a serve still answers for it.
	stopped := false
	if pid := recordedPID(st.Dir()); pid > 0 && serveAnswers(st.Dir()) {
		if err := endProcess(pid); err != nil {
			return fmt.Errorf("stopping serve (pid %d): %w", pid, err)
		}
		for i := 0; i < 50 && alive(pid) && serveAnswers(st.Dir()); i++ {
			time.Sleep(100 * time.Millisecond)
		}
		stopped = true
	}
	switch {
	case stopped || removed:
		fmt.Fprintln(stdout, "stopped; it no longer starts when you log in. Start it again with: herdr-anywhere start")
	default:
		fmt.Fprintln(stdout, "not running, and not set to start when you log in")
	}
	return nil
}

// recordedPID is the process serve recorded in serve.json, or 0.
func recordedPID(dir string) int {
	b, err := os.ReadFile(filepath.Join(dir, serveURLsFile))
	if err != nil {
		return 0
	}
	var f struct {
		PID int `json:"pid"`
	}
	if json.Unmarshal(b, &f) != nil {
		return 0
	}
	return f.PID
}

// serveAnswers: a serve answers on dir, by Tailcat's control socket or on the address it recorded.
func serveAnswers(dir string) bool {
	return tailcat.ServeRunning(dir) || readServeURLs(dir) != nil
}

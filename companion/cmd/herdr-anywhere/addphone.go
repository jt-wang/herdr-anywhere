package main

// What the Add phone dialog in the Herdr desktop app runs: pair and devices
// as JSON, start, and install-dialog, which puts the dialog in place.

import (
	"context"
	"embed"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"time"

	"rsc.io/qr"

	"herdr-anywhere/companion/internal/store"
	"herdr-anywhere/companion/internal/tailcat"
)

// errReported means the failure is already on stdout, as JSON.
var errReported = errors.New("reported")

// execLimit stays under the 30 s Herdr gives a program a mod runs.
const execLimit = 25 * time.Second

func reportJSON(stdout io.Writer, message string) error {
	_ = json.NewEncoder(stdout).Encode(map[string]string{"error": message})
	return errReported
}

// pairJSON prints the invite and its QR as rows of 0 and 1. The invite stays
// in this process's memory and on stdout: nothing here writes it to disk.
func pairJSON(st *store.Store, name string, ttl time.Duration, stdout io.Writer) error {
	if !tailcat.ServeRunning(st.Dir()) {
		return reportJSON(stdout, "not-running")
	}
	code, err := st.CreateCode(name, ttl)
	if err != nil {
		return reportJSON(stdout, err.Error())
	}
	ctx, cancel := context.WithTimeout(context.Background(), execLimit)
	defer cancel()
	reply, err := tailcat.RequestPair(ctx, st.Dir(), code)
	if errors.Is(err, tailcat.ErrNoServe) {
		return reportJSON(stdout, "not-running")
	}
	if err != nil {
		return reportJSON(stdout, err.Error())
	}
	matrix, err := qr.Encode(reply.Invite, qr.L)
	if err != nil {
		return reportJSON(stdout, err.Error())
	}
	rows := make([]string, matrix.Size)
	for y := range rows {
		row := make([]byte, matrix.Size)
		for x := range row {
			row[x] = '0'
			if matrix.Black(x, y) {
				row[x] = '1'
			}
		}
		rows[y] = string(row)
	}
	return json.NewEncoder(stdout).Encode(map[string]any{"invite": reply.Invite, "expires": reply.Expires, "qr": rows})
}

func devicesJSON(ds []store.Device, stdout io.Writer) error {
	type row struct {
		ID       string    `json:"id"`
		Name     string    `json:"name"`
		Created  time.Time `json:"created"`
		LastSeen time.Time `json:"last_seen,omitzero"`
	}
	rows := make([]row, 0, len(ds))
	for _, d := range ds {
		rows = append(rows, row{d.ID, d.Name, d.Created, d.LastSeen})
	}
	return json.NewEncoder(stdout).Encode(rows)
}

// --- start ---------------------------------------------------------------

const serveLogFile = "serve.log"

// startWait is how long start waits for serve to answer.
var startWait = execLimit

// startedServeArgs asks for any free local port: phones arrive through Tailcat.
func startedServeArgs(dir string) []string {
	return []string{"serve", "--tailcat", "--listen", "127.0.0.1:0", "--state", dir}
}

// spawnServe runs serve --tailcat in a session of its own, so it outlives
// whoever asked: the dialog, and Herdr. It returns serve's exit and a way to stop it.
var spawnServe = func(dir string, logFile *os.File) (<-chan error, func(), error) {
	self, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.Command(self, startedServeArgs(dir)...)
	cmd.Stdout, cmd.Stderr = logFile, logFile
	cmd.SysProcAttr = detached()
	if err := cmd.Start(); err != nil {
		return nil, nil, err
	}
	exited := make(chan error, 1)
	go func() { exited <- cmd.Wait() }()
	return exited, func() { _ = cmd.Process.Kill() }, nil
}

func start(args []string, stdout, stderr io.Writer) error {
	fs, state := newFlags("start", stderr)
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
	if tailcat.ServeRunning(st.Dir()) {
		fmt.Fprintln(stdout, "already running")
		return nil
	}
	logPath := filepath.Join(st.Dir(), serveLogFile)
	logFile, err := os.OpenFile(logPath, os.O_CREATE|os.O_WRONLY|os.O_TRUNC, 0o600)
	if err != nil {
		return err
	}
	defer logFile.Close()
	exited, stop, err := spawnServe(st.Dir(), logFile)
	if err != nil {
		return err
	}
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
			return fmt.Errorf("serve did not answer within %s; see %s", startWait, logPath)
		case <-tick.C:
			if tailcat.ServeRunning(st.Dir()) {
				fmt.Fprintln(stdout, "started")
				return nil
			}
		}
	}
}

func lastLine(path string) string {
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(strings.TrimSpace(string(b)), "\n")
	return lines[len(lines)-1]
}

// --- install-dialog ------------------------------------------------------

//go:embed dialog/main.js dialog/logic.js dialog/mod.json
var dialogFiles embed.FS

const (
	dialogMod    = "add-phone"
	dialogAction = "mod.open:" + dialogMod
	dialogTitle  = `"title": "Add phone"`
)

// dialogKey opens the dialog, spelled as Herdr's own keys are on this platform.
var dialogKey = dialogKeyFor(runtime.GOOS)

// dialogKeyFor: Herdr on macOS binds its keys as cmd-<key>; on Windows and
// Linux as ctrl-shift-<key> (herdr-app 0.1.13's defaults/keys.json there).
func dialogKeyFor(goos string) string {
	if goos == "darwin" {
		return "cmd-shift-m"
	}
	return "ctrl-shift-m"
}

func installDialog(args []string, stdout, stderr io.Writer) error {
	fs, state := newFlags("install-dialog", stderr)
	appHome := fs.String("app-home", "", "Herdr's home (default $HERDR_APP_HOME, else ~/.config/herdr-app)")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return usageError{fmt.Errorf("unexpected argument %q", fs.Arg(0))}
	}
	home := *appHome
	if home == "" {
		home = os.Getenv("HERDR_APP_HOME")
	}
	if home == "" {
		h, err := os.UserHomeDir()
		if err != nil {
			return err
		}
		home = filepath.Join(h, ".config", "herdr-app")
	}
	keysPath := filepath.Join(home, "keys.json")
	keys, err := os.ReadFile(keysPath)
	if errors.Is(err, os.ErrNotExist) {
		return fmt.Errorf("no %s. Open Herdr once, then run this again", keysPath)
	}
	if err != nil {
		return err
	}
	newKeys, bound, err := withDialogKey(keys)
	if err != nil {
		return fmt.Errorf("%s: %w", keysPath, err)
	}
	dir := filepath.Join(home, "mods", dialogMod)
	if b, err := os.ReadFile(filepath.Join(dir, "mod.json")); err == nil && !strings.Contains(string(b), dialogTitle) {
		return fmt.Errorf("%s is another mod; move it and run this again", dir)
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	self, err := os.Executable()
	if err != nil {
		return err
	}
	statePath := *state
	if statePath == "" {
		statePath = os.Getenv("HERDR_ANYWHERE_STATE")
	}
	for _, name := range []string{"logic.js", "mod.json", "main.js"} {
		b, err := dialogFiles.ReadFile("dialog/" + name)
		if err != nil {
			return err
		}
		if name == "main.js" { // last: config.js must exist when Herdr loads it
			if err := writeDialogConfig(dir, self, statePath); err != nil {
				return err
			}
		}
		if err := os.WriteFile(filepath.Join(dir, name), b, 0o644); err != nil {
			return err
		}
	}
	if newKeys != nil {
		if err := writeAtomic(keysPath, newKeys); err != nil {
			return err
		}
	}
	if bound == "" {
		fmt.Fprintf(stdout, "Add phone is installed, but %s is taken in %s. Bind a free key to %q there.\n", dialogKey, keysPath, dialogAction)
		return nil
	}
	fmt.Fprintf(stdout, "Add phone is installed. Press %s in Herdr to open it.\nThe dialog runs %s to pair, list and remove phones.\n", bound, self)
	return nil
}

func writeDialogConfig(dir, bin, state string) error {
	b, _ := json.Marshal(bin)
	s, _ := json.Marshal(state)
	text := fmt.Sprintf("// Written by herdr-anywhere install-dialog.\nexport const BIN = %s;\nexport const STATE = %s;\n", b, s)
	return os.WriteFile(filepath.Join(dir, "config.js"), []byte(text), 0o644)
}

func writeAtomic(path string, b []byte) error {
	tmp := path + ".add-phone"
	if err := os.WriteFile(tmp, b, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

var globalSection = regexp.MustCompile(`"global"\s*:\s*\{`)

// withDialogKey returns keys.json with the dialog's key added to the global
// section as one new line, leaving the rest of the file as the person has it,
// and the key that opens the dialog. nil bytes mean nothing needs writing; an
// empty key means the key is taken by something else.
func withDialogKey(keys []byte) ([]byte, string, error) {
	var parsed map[string]map[string]any
	if err := json.Unmarshal(keys, &parsed); err != nil {
		return nil, "", fmt.Errorf("cannot read it: %w", err)
	}
	for key, action := range parsed["global"] {
		if action == dialogAction {
			return nil, key, nil
		}
	}
	if _, taken := parsed["global"][dialogKey]; taken {
		return nil, "", nil
	}
	loc := globalSection.FindIndex(keys)
	if loc == nil {
		return nil, "", errors.New("it has no global section")
	}
	line := fmt.Sprintf("\n    %q: %q,", dialogKey, dialogAction)
	out := append(append(append([]byte{}, keys[:loc[1]]...), line...), keys[loc[1]:]...)
	if len(parsed["global"]) == 0 { // an empty section takes no trailing comma
		line = strings.TrimSuffix(line, ",")
		out = append(append(append([]byte{}, keys[:loc[1]]...), line...), keys[loc[1]:]...)
	}
	var check map[string]map[string]any
	if err := json.Unmarshal(out, &check); err != nil || check["global"][dialogKey] != dialogAction {
		return nil, "", errors.New("could not add the key to it")
	}
	return out, dialogKey, nil
}

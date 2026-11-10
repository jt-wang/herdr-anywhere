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

	"herdr-anywhere/companion/internal/lan"
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

// serveOpts are what start was given for serve beyond Tailcat: --lan, --listen. Only this start's go into the login
// entry; nothing is kept from an earlier one.
type serveOpts struct {
	lan    bool
	listen []string
}

// flags is opts as start's own flags, to tell the person the command that applies them.
func (o serveOpts) flags() string {
	var b strings.Builder
	for _, l := range o.listen {
		b.WriteString(" --listen " + l)
	}
	if o.lan {
		b.WriteString(" --lan")
	}
	return b.String()
}

// startedServeArgs is the serve start runs. With neither --lan nor --listen it asks for any free local port: phones
// arrive through Tailcat. --lan keeps serve's own port, 7681: a LAN phone keeps the address it paired with, and a port
// that changed at each login would lose it.
func startedServeArgs(dir string, o serveOpts) []string {
	args := []string{"serve", "--tailcat"}
	if !o.lan && len(o.listen) == 0 {
		args = append(args, "--listen", "127.0.0.1:0")
	}
	for _, l := range o.listen {
		args = append(args, "--listen", l)
	}
	if o.lan {
		args = append(args, "--lan")
	}
	return append(args, "--state", dir)
}

// lanAddresses are this computer's private LAN addresses; a var for tests.
var lanAddresses = lan.SystemAddresses

// spawnServe runs serve with args in a session of its own, so it outlives whoever asked: the dialog, and Herdr. It
// returns serve's exit and a way to stop it.
var spawnServe = func(args []string, logFile *os.File) (<-chan error, func(), error) {
	self, err := os.Executable()
	if err != nil {
		return nil, nil, err
	}
	cmd := exec.Command(self, args...)
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
	var listen listFlag
	fs.Var(&listen, "listen", "also listen on this address, as serve --listen (repeatable); kept for each login")
	withLAN := fs.Bool("lan", false, "also listen on this computer's private LAN address(es) on serve's port (7681, or the first --listen's), as serve --lan; kept for each login")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return usageError{fmt.Errorf("unexpected argument %q", fs.Arg(0))}
	}
	o := serveOpts{lan: *withLAN, listen: listen}
	// What serve would refuse fails here, not later in the background, at every login.
	if _, err := listenAddrs(o.listen, false, nil); err != nil {
		return usageError{err}
	}
	if o.lan {
		addrs, err := lanAddresses()
		if err != nil {
			return err
		}
		if _, err := listenAddrs(o.listen, true, addrs); err != nil {
			return err
		}
	}
	st, err := openStore(*state)
	if err != nil {
		return err
	}
	if err := startCompanion(st, o, stdout, stderr); err != nil {
		return err
	}
	startDialog(*state, stdout, stderr)
	return nil
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

//go:embed dialog/main.js dialog/logic.js dialog/strings.js dialog/mod.json
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
	ifInstalled := fs.Bool("if-installed", false, "only bring an installed dialog up to date (update runs this)")
	if err := parse(fs, args); err != nil {
		return err
	}
	if fs.NArg() > 0 {
		return usageError{fmt.Errorf("unexpected argument %q", fs.Arg(0))}
	}
	home, err := herdrAppHome(*appHome)
	if err != nil {
		return err
	}
	if *ifInstalled {
		changed, err := refreshDialog(home)
		if changed {
			fmt.Fprintln(stdout, "Add phone in Herdr Desktop App is up to date too.")
		}
		return err
	}
	d, err := placeDialog(home, *state, true)
	if errors.Is(err, errNoDesktopApp) {
		return fmt.Errorf("no %s. Open Herdr once, then run this again", d.keys)
	}
	if err != nil {
		return err
	}
	if d.key == "" {
		fmt.Fprintf(stdout, "Add phone is installed, but %s is taken in %s. Bind a free key to %q there.\n", dialogKey, d.keys, dialogAction)
		return nil
	}
	fmt.Fprintf(stdout, "Add phone is installed. Press %s in Herdr to open it.\nThe dialog runs %s to pair, list and remove phones.\n", d.key, d.bin)
	return nil
}

// herdrAppHome is the Herdr desktop app's home: flag, else $HERDR_APP_HOME, else ~/.config/herdr-app.
func herdrAppHome(flag string) (string, error) {
	if flag != "" {
		return flag, nil
	}
	if env := os.Getenv("HERDR_APP_HOME"); env != "" {
		return env, nil
	}
	h, err := os.UserHomeDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(h, ".config", "herdr-app"), nil
}

// errNoDesktopApp: the Herdr desktop app has never run here (no keys.json), so there is nowhere to put the dialog.
var errNoDesktopApp = errors.New("no Herdr desktop app")

// placedDialog is what placeDialog did: the key that opens the dialog ("" for none), keys.json, the program the dialog runs.
type placedDialog struct{ key, keys, bin string }

// dialogFileNames are the files of the dialog, config.js last: main.js imports it, so it exists before main.js changes.
var dialogFileNames = []string{"strings.js", "logic.js", "mod.json", "main.js"}

// placeDialog puts this version's dialog in home, running this program on state. addKey binds dialogKey when no key
// opens the dialog (install-dialog); start passes false for a dialog already there, whose keys the person has made
// their own. A file already as it should be is not written again: Herdr may be running it.
func placeDialog(home, state string, addKey bool) (placedDialog, error) {
	d := placedDialog{keys: filepath.Join(home, "keys.json")}
	keys, err := os.ReadFile(d.keys)
	if errors.Is(err, os.ErrNotExist) {
		return d, errNoDesktopApp
	}
	if err != nil {
		return d, err
	}
	newKeys, bound, err := withDialogKey(keys)
	if err != nil {
		return d, fmt.Errorf("%s: %w", d.keys, err)
	}
	dir := filepath.Join(home, "mods", dialogMod)
	if b, err := os.ReadFile(filepath.Join(dir, "mod.json")); err == nil && !strings.Contains(string(b), dialogTitle) {
		return d, fmt.Errorf("%s is another mod; move it and run this again", dir)
	}
	if !addKey && newKeys != nil {
		newKeys, bound = nil, ""
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return d, err
	}
	if d.bin, err = os.Executable(); err != nil {
		return d, err
	}
	if state == "" {
		state = os.Getenv("HERDR_ANYWHERE_STATE")
	}
	for _, name := range dialogFileNames {
		if name == "main.js" {
			if _, err := writeIfChanged(filepath.Join(dir, "config.js"), dialogConfig(d.bin, state)); err != nil {
				return d, err
			}
		}
		b, err := dialogFiles.ReadFile("dialog/" + name)
		if err != nil {
			return d, err
		}
		if _, err := writeIfChanged(filepath.Join(dir, name), b); err != nil {
			return d, err
		}
	}
	if newKeys != nil {
		if err := writeAtomic(d.keys, newKeys); err != nil {
			return d, err
		}
	}
	d.key = bound
	return d, nil
}

// refreshDialog brings an installed dialog's files up to date, leaving config.js and keys.json as they are; with no
// dialog of ours in home it does nothing. It says whether it changed a file.
func refreshDialog(home string) (bool, error) {
	dir := filepath.Join(home, "mods", dialogMod)
	if b, err := os.ReadFile(filepath.Join(dir, "mod.json")); err != nil || !strings.Contains(string(b), dialogTitle) {
		return false, nil
	}
	if _, err := os.Stat(filepath.Join(dir, "config.js")); err != nil {
		return false, nil // main.js would import a config.js that isn't there
	}
	changed := false
	for _, name := range dialogFileNames {
		b, err := dialogFiles.ReadFile("dialog/" + name)
		if err != nil {
			return changed, err
		}
		c, err := writeIfChanged(filepath.Join(dir, name), b)
		changed = changed || c
		if err != nil {
			return changed, err
		}
	}
	return changed, nil
}

func writeIfChanged(path string, b []byte) (bool, error) {
	if old, err := os.ReadFile(path); err == nil && string(old) == string(b) {
		return false, nil
	}
	return true, os.WriteFile(path, b, 0o644)
}

// startDialog is start's part for the Herdr desktop app: put this version's dialog in place, and say how to
// open it. Nothing here stops start: without the app, it says how to get the dialog, or to pair from the terminal.
func startDialog(state string, stdout, stderr io.Writer) {
	home, err := herdrAppHome("")
	if err != nil {
		fmt.Fprintf(stderr, "herdr-anywhere: the Add phone dialog: %v\n", err)
		return
	}
	_, statErr := os.Stat(filepath.Join(home, "mods", dialogMod, "mod.json"))
	d, err := placeDialog(home, state, statErr != nil)
	switch {
	case errors.Is(err, errNoDesktopApp):
		fmt.Fprintf(stdout, "To pair a phone, run herdr-anywhere pair. Herdr Desktop App isn't set up on this computer (no %s);\n"+
			"with it, a key opens an Add phone dialog instead: install it from https://github.com/herdrdev/herdr-app, open it once,\n"+
			"then run herdr-anywhere start again.\n", d.keys)
	case err != nil:
		fmt.Fprintf(stderr, "herdr-anywhere: could not put the Add phone dialog in Herdr Desktop App: %v\n", err)
	case d.key == "":
		fmt.Fprintf(stdout, "To add a phone, open Add phone in Herdr Desktop App: no key opens it yet, so bind one to %q in %s.\n", dialogAction, d.keys)
	default:
		fmt.Fprintf(stdout, "To add a phone, press %s in Herdr Desktop App.\n", d.key)
	}
}

func dialogConfig(bin, state string) []byte {
	b, _ := json.Marshal(bin)
	s, _ := json.Marshal(state)
	return fmt.Appendf(nil, "// Written by herdr-anywhere install-dialog.\nexport const BIN = %s;\nexport const STATE = %s;\n", b, s)
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

package main

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"rsc.io/qr"

	"herdr-anywhere/companion/internal/store"
	"herdr-anywhere/companion/internal/tailcat"
	"herdr-anywhere/companion/internal/testfake"
)

type pairReplyJSON struct {
	Invite  string    `json:"invite"`
	Expires time.Time `json:"expires"`
	QR      []string  `json:"qr"`
	Error   string    `json:"error"`
}

func TestPairJSONGivesTheInviteAndTheQRMatrix(t *testing.T) {
	dir := shortStateDir(t)
	got := fakeTailcatServe(t, dir, nil)
	out, errs, code := runCLI(t, "pair", "--json", "--state", dir, "--name", "My Phone")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	var r pairReplyJSON
	if err := json.Unmarshal([]byte(out), &r); err != nil {
		t.Fatalf("not JSON: %v\n%s", err, out)
	}
	if r.Invite != tailcat.Prefix+"INVITE" || time.Until(r.Expires) <= 0 {
		t.Fatalf("invite %q expires %v", r.Invite, r.Expires)
	}
	want, err := qr.Encode(r.Invite, qr.L)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.QR) != want.Size {
		t.Fatalf("%d rows, want %d", len(r.QR), want.Size)
	}
	for y, row := range r.QR {
		if len(row) != want.Size {
			t.Fatalf("row %d is %d wide, want %d", y, len(row), want.Size)
		}
		for x, c := range row {
			if (c == '1') != want.Black(x, y) || (c != '0' && c != '1') {
				t.Fatalf("module %d,%d is %q", x, y, c)
			}
		}
	}
	st, _ := store.Open(dir)
	if d, _, err := st.Redeem(*got, ""); err != nil || d.Name != "My Phone" {
		t.Fatalf("the code serve got does not pair: %+v %v", d, err)
	}
	if entries, _ := os.ReadDir(dir); len(entries) > 0 {
		for _, e := range entries {
			b, _ := os.ReadFile(filepath.Join(dir, e.Name()))
			if strings.Contains(string(b), r.Invite) {
				t.Fatalf("%s holds the invite", e.Name())
			}
		}
	}
}

func TestPairJSONWithoutServeSaysSoAndCreatesNoCode(t *testing.T) {
	dir := shortStateDir(t)
	out, _, code := runCLI(t, "pair", "--json", "--state", dir)
	if code != 1 {
		t.Fatalf("exit %d, want 1", code)
	}
	var r pairReplyJSON
	if err := json.Unmarshal([]byte(out), &r); err != nil || r.Error != "not-running" {
		t.Fatalf("reply %q (%v)", out, err)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "state.json"))
	if strings.Contains(string(b), "sha256") {
		t.Fatalf("a code was created: %s", b)
	}
}

func TestPairJSONReportsWhatServeSaid(t *testing.T) {
	dir := shortStateDir(t)
	ln, err := tailcat.Listen(dir)
	if err != nil {
		t.Fatal(err)
	}
	open := func(context.Context, string) (tailcat.Window, error) {
		return tailcat.Window{}, errors.New("relay down")
	}
	srv := &http.Server{Handler: tailcat.ControlHandler(open, nil)}
	go srv.Serve(ln)
	t.Cleanup(func() { srv.Close() })
	out, _, code := runCLI(t, "pair", "--json", "--state", dir)
	var r pairReplyJSON
	if code != 1 || json.Unmarshal([]byte(out), &r) != nil || !strings.Contains(r.Error, "relay down") {
		t.Fatalf("exit %d, reply %q", code, out)
	}
}

func TestServeRunningIsFalseForASocketACrashedServeLeft(t *testing.T) {
	dir := shortStateDir(t)
	if tailcat.ServeRunning(dir) {
		t.Fatal("running with no socket")
	}
	ln, err := net.ListenUnix("unix", &net.UnixAddr{Name: filepath.Join(dir, tailcat.SocketName), Net: "unix"})
	if err != nil {
		t.Fatal(err)
	}
	if !tailcat.ServeRunning(dir) {
		t.Fatal("not running while the socket answers")
	}
	ln.SetUnlinkOnClose(false)
	ln.Close()
	if _, err := os.Stat(filepath.Join(dir, tailcat.SocketName)); err != nil {
		t.Fatal("the stale socket should still be there:", err)
	}
	if tailcat.ServeRunning(dir) {
		t.Fatal("running with only a stale socket")
	}
}

func TestDevicesJSON(t *testing.T) {
	dir := shortStateDir(t)
	out, errs, code := runCLI(t, "devices", "--json", "--state", dir)
	if code != 0 || strings.TrimSpace(out) != "[]" {
		t.Fatalf("exit %d, out %q, err %q", code, out, errs)
	}
	st, _ := store.Open(dir)
	c, _ := st.CreateCode("Phone", time.Minute)
	d, _, err := st.RedeemTailcat(c, "Pixel", "nodekey:SECRET")
	if err != nil {
		t.Fatal(err)
	}
	out, errs, code = runCLI(t, "devices", "--json", "--state", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	var ds []map[string]any
	if err := json.Unmarshal([]byte(out), &ds); err != nil {
		t.Fatalf("%v: %s", err, out)
	}
	if len(ds) != 1 || ds[0]["id"] != d.ID || ds[0]["name"] != "Pixel" || ds[0]["created"] == nil {
		t.Fatalf("devices %s", out)
	}
	if strings.Contains(out, "nodekey") {
		t.Fatalf("the Tailcat key is printed: %s", out)
	}
}

func TestStartReturnsAtOnceWhenServeIsRunning(t *testing.T) {
	dir := shortStateDir(t)
	fakeTailcatServe(t, dir, nil)
	restore := spawnServe
	t.Cleanup(func() { spawnServe = restore })
	spawnServe = func(string, *os.File) (<-chan error, func(), error) {
		t.Fatal("spawned a second serve")
		return nil, nil, nil
	}
	out, errs, code := runCLI(t, "start", "--state", dir)
	if code != 0 || !strings.Contains(out, "already running") {
		t.Fatalf("exit %d, out %q, err %q", code, out, errs)
	}
}

func TestStartWaitsForServeToAnswer(t *testing.T) {
	dir := shortStateDir(t)
	restore := spawnServe
	t.Cleanup(func() { spawnServe = restore })
	spawnServe = func(string, *os.File) (<-chan error, func(), error) {
		time.AfterFunc(300*time.Millisecond, func() { fakeTailcatServe(t, dir, nil) })
		return make(chan error), func() {}, nil
	}
	_, errs, code := runCLI(t, "start", "--state", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if !tailcat.ServeRunning(dir) {
		t.Fatal("start returned before serve answered")
	}
}

func TestStartShowsWhyServeDied(t *testing.T) {
	dir := shortStateDir(t)
	restore := spawnServe
	t.Cleanup(func() { spawnServe = restore })
	spawnServe = func(_ string, log *os.File) (<-chan error, func(), error) {
		log.WriteString("listening went wrong\nbind: address already in use\n")
		c := make(chan error, 1)
		c <- errors.New("exit status 1")
		return c, func() {}, nil
	}
	_, errs, code := runCLI(t, "start", "--state", dir)
	if code != 1 || !strings.Contains(errs, "address already in use") {
		t.Fatalf("exit %d: %s", code, errs)
	}
	fi, err := os.Stat(filepath.Join(dir, "serve.log"))
	if err != nil || (testfake.PermBits() && fi.Mode().Perm() != 0o600) {
		t.Fatalf("serve.log: %v %v", fi, err)
	}
}

func TestStartStopsAServeThatNeverAnswers(t *testing.T) {
	dir := shortStateDir(t)
	restore, wait := spawnServe, startWait
	t.Cleanup(func() { spawnServe, startWait = restore, wait })
	startWait = 200 * time.Millisecond
	stopped := false
	spawnServe = func(string, *os.File) (<-chan error, func(), error) {
		return make(chan error), func() { stopped = true }, nil
	}
	_, errs, code := runCLI(t, "start", "--state", dir)
	if code != 1 || !stopped || !strings.Contains(errs, "did not answer") {
		t.Fatalf("exit %d, stopped %v: %s", code, stopped, errs)
	}
}

func TestStartNeedsNoFixedPort(t *testing.T) {
	// Phones come through Tailcat, so another program on 7681 must not stop it.
	args := strings.Join(startedServeArgs("/some/state"), " ")
	if !strings.Contains(args, "--tailcat") || !strings.Contains(args, "--listen 127.0.0.1:0") || !strings.Contains(args, "--state /some/state") {
		t.Fatalf("serve args: %s", args)
	}
}

func TestDialogUnderNode(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Skip("no node")
	}
	cmd := exec.Command(node, "--test")
	cmd.Dir = "dialog"
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("%v\n%s", err, out)
	}
}

func homeWithKeys(t *testing.T, keys string) string {
	t.Helper()
	home := t.TempDir()
	if keys != "" {
		if err := os.WriteFile(filepath.Join(home, "keys.json"), []byte(keys), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

const someKeys = "{\n  \"global\": {\n    \"cmd-t\": \"tab.new\",\n    \"cmd-p\": \"mod.open:picker\"\n  },\n  \"terminal\": {\n    \"cmd-c\": \"terminal.copy\"\n  }\n}\n"

func TestInstallDialogAddsTheModAndItsKey(t *testing.T) {
	home := homeWithKeys(t, someKeys)
	out, errs, code := runCLI(t, "install-dialog", "--app-home", home, "--state", "/some/state")
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	for _, f := range []string{"main.js", "logic.js", "mod.json", "config.js"} {
		if _, err := os.Stat(filepath.Join(home, "mods", "add-phone", f)); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "mods", "add-phone", "logic.test.mjs")); err == nil {
		t.Fatal("the test was installed")
	}
	cfg, _ := os.ReadFile(filepath.Join(home, "mods", "add-phone", "config.js"))
	self, _ := os.Executable()
	selfJSON, _ := json.Marshal(self) // Windows paths' backslashes are escaped in config.js
	if !strings.Contains(string(cfg), string(selfJSON)) || !strings.Contains(string(cfg), "/some/state") {
		t.Fatalf("config.js: %s", cfg)
	}
	b, _ := os.ReadFile(filepath.Join(home, "keys.json"))
	var keys map[string]map[string]string
	if err := json.Unmarshal(b, &keys); err != nil {
		t.Fatalf("%v: %s", err, b)
	}
	if keys["global"][dialogKey] != "mod.open:add-phone" || keys["global"]["cmd-t"] != "tab.new" || keys["terminal"]["cmd-c"] != "terminal.copy" {
		t.Fatalf("keys: %s", b)
	}
	if strings.Index(string(b), "cmd-p") > strings.Index(string(b), "terminal.copy") {
		t.Fatalf("the file was reordered: %s", b)
	}
	if !strings.Contains(out, dialogKey) {
		t.Fatalf("does not say how to open it: %s", out)
	}
	if _, _, code = runCLI(t, "install-dialog", "--app-home", home, "--state", "/some/state"); code != 0 {
		t.Fatal("a second install failed")
	}
	again, _ := os.ReadFile(filepath.Join(home, "keys.json"))
	if string(again) != string(b) {
		t.Fatalf("a second install changed keys.json: %s", again)
	}
}

func TestInstallDialogLeavesAKeyThePersonUses(t *testing.T) {
	keys := strings.Replace(someKeys, `"cmd-t"`, `"`+dialogKey+`"`, 1)
	home := homeWithKeys(t, keys)
	out, errs, code := runCLI(t, "install-dialog", "--app-home", home)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	b, _ := os.ReadFile(filepath.Join(home, "keys.json"))
	if string(b) != keys {
		t.Fatalf("keys.json changed: %s", b)
	}
	if !strings.Contains(out, "mod.open:add-phone") {
		t.Fatalf("does not say what to bind: %s", out)
	}
}

func TestInstallDialogWillNotReplaceSomeoneElsesMod(t *testing.T) {
	home := homeWithKeys(t, someKeys)
	dir := filepath.Join(home, "mods", "add-phone")
	os.MkdirAll(dir, 0o755)
	os.WriteFile(filepath.Join(dir, "mod.json"), []byte(`{"place":"right"}`), 0o644)
	_, errs, code := runCLI(t, "install-dialog", "--app-home", home)
	if code != 1 || !strings.Contains(errs, "add-phone") {
		t.Fatalf("exit %d: %s", code, errs)
	}
	b, _ := os.ReadFile(filepath.Join(dir, "mod.json"))
	if string(b) != `{"place":"right"}` {
		t.Fatalf("replaced: %s", b)
	}
}

func TestInstallDialogWantsHerdrToHaveRunOnce(t *testing.T) {
	home := homeWithKeys(t, "")
	_, errs, code := runCLI(t, "install-dialog", "--app-home", home)
	if code != 1 || !strings.Contains(errs, "Open Herdr once") {
		t.Fatalf("exit %d: %s", code, errs)
	}
}

func TestInstallDialogLeavesKeysItCannotRead(t *testing.T) {
	home := homeWithKeys(t, "{ not json")
	_, errs, code := runCLI(t, "install-dialog", "--app-home", home)
	if code != 1 || !strings.Contains(errs, "keys.json") {
		t.Fatalf("exit %d: %s", code, errs)
	}
	b, _ := os.ReadFile(filepath.Join(home, "keys.json"))
	if string(b) != "{ not json" {
		t.Fatalf("changed: %s", b)
	}
}

// Herdr for Windows and Linux binds its own keys as ctrl-shift-<key>: the
// defaults/keys.json in herdr-app 0.1.13 for each has ctrl-shift-t, -o, -c and
// no cmd- chords, and its release notes give Ctrl+Shift+F for what macOS has
// as Cmd+F. ctrl-shift-m is not among those defaults.
func TestDialogKeyPerPlatform(t *testing.T) {
	for goos, want := range map[string]string{"darwin": "cmd-shift-m", "windows": "ctrl-shift-m", "linux": "ctrl-shift-m"} {
		if got := dialogKeyFor(goos); got != want {
			t.Errorf("dialogKeyFor(%q) = %q, want %q", goos, got, want)
		}
	}
}

package main

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/autostart"
	"herdr-anywhere/companion/internal/testfake"
)

// TestMain keeps every test away from the Herdr desktop app of whoever runs them: start, serve and update put the Add
// phone dialog in $HERDR_APP_HOME, which points at an empty folder here unless a test sets its own. And from their
// Companion's entry at login: the tests' start, pair and stop use a fake (atLogin, in login_test.go), a real one
// would get a name of the run's own, and the run fails when the person's entry changed or one was loaded or unloaded.
func TestMain(m *testing.M) {
	home, err := os.MkdirTemp("", "hm-app-home")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	os.Setenv("HERDR_APP_HOME", home)
	own := fmt.Sprintf("io.example.herdranywhere.test.go-main.%d", os.Getpid())
	os.Setenv(autostart.NameEnv, own)
	guard := testfake.GuardAutostart(own)
	code := m.Run()
	os.RemoveAll(home)
	for _, bad := range guard.Check() {
		fmt.Fprintln(os.Stderr, "FAIL: the person's Companion entry at login:", bad)
		code = 1
	}
	os.Exit(code)
}

// appHomeWith is a Herdr desktop app home for this test, with keys.json holding keys ("" for none).
func appHomeWith(t *testing.T, keys string) string {
	t.Helper()
	home := homeWithKeys(t, keys)
	t.Setenv("HERDR_APP_HOME", home)
	return home
}

func modFile(home, name string) string { return filepath.Join(home, "mods", dialogMod, name) }

func embedded(t *testing.T, name string) string {
	t.Helper()
	b, err := dialogFiles.ReadFile("dialog/" + name)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// oldDialog is the dialog as an earlier version installed it: other main.js and logic.js, bound to key.
func oldDialog(t *testing.T, key string) string {
	t.Helper()
	keys := someKeys
	if key != "" {
		keys = strings.Replace(someKeys, `"cmd-t": "tab.new"`, fmt.Sprintf("%q: %q", key, dialogAction), 1)
	}
	home := appHomeWith(t, keys)
	os.MkdirAll(modFile(home, ""), 0o755)
	for name, text := range map[string]string{"main.js": "// old main\n", "logic.js": "// old logic\n", "mod.json": embedded(t, "mod.json"),
		"config.js": "export const BIN = \"/old/herdr-anywhere\";\nexport const STATE = \"\";\n"} {
		if err := os.WriteFile(modFile(home, name), []byte(text), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	return home
}

func read(t *testing.T, path string) string {
	t.Helper()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return string(b)
}

// `start` puts the Add phone dialog in the Herdr desktop app and says which key opens it.
func TestStartInstallsTheDialog(t *testing.T) {
	home := appHomeWith(t, someKeys)
	dir := shortStateDir(t)
	fakeTailcatServe(t, dir, nil)
	out, errs, code := runCLI(t, "start", "--state", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	for _, f := range []string{"main.js", "logic.js", "mod.json"} {
		if read(t, modFile(home, f)) != embedded(t, f) {
			t.Fatalf("%s is not this version's", f)
		}
	}
	self, _ := os.Executable()
	selfJSON, _ := json.Marshal(self)
	dirJSON, _ := json.Marshal(dir)
	if cfg := read(t, modFile(home, "config.js")); !strings.Contains(cfg, string(selfJSON)) || !strings.Contains(cfg, string(dirJSON)) {
		t.Fatalf("config.js does not run this program on start's state: %s", cfg)
	}
	var keys map[string]map[string]string
	if err := json.Unmarshal([]byte(read(t, filepath.Join(home, "keys.json"))), &keys); err != nil || keys["global"][dialogKey] != dialogAction {
		t.Fatalf("keys.json: %v %v", keys, err)
	}
	if !strings.Contains(out, "already running") || !strings.Contains(out, "press "+dialogKey+" in Herdr Desktop App") {
		t.Fatalf("does not say how to open the dialog:\n%s", out)
	}
}

// An older dialog gets this version's files; the key the person has for it stays, and start names that one.
func TestStartRefreshesAnOldDialog(t *testing.T) {
	home := oldDialog(t, "cmd-k")
	keysBefore := read(t, filepath.Join(home, "keys.json"))
	dir := shortStateDir(t)
	fakeTailcatServe(t, dir, nil)
	out, errs, code := runCLI(t, "start", "--state", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if read(t, modFile(home, "main.js")) != embedded(t, "main.js") || read(t, modFile(home, "logic.js")) != embedded(t, "logic.js") {
		t.Fatal("the old dialog is still there")
	}
	if strings.Contains(read(t, modFile(home, "config.js")), "/old/herdr-anywhere") {
		t.Fatal("config.js still runs the old program")
	}
	if read(t, filepath.Join(home, "keys.json")) != keysBefore {
		t.Fatal("start changed keys.json")
	}
	if !strings.Contains(out, "press cmd-k in Herdr Desktop App") {
		t.Fatalf("does not name the person's key:\n%s", out)
	}
}

// Someone who took the dialog's key out of keys.json did it on purpose: start does not put it back, and says what to bind.
func TestStartLeavesTheKeysOfAnInstalledDialog(t *testing.T) {
	home := oldDialog(t, "")
	dir := shortStateDir(t)
	fakeTailcatServe(t, dir, nil)
	out, errs, code := runCLI(t, "start", "--state", dir)
	if code != 0 {
		t.Fatalf("exit %d: %s", code, errs)
	}
	if read(t, filepath.Join(home, "keys.json")) != someKeys {
		t.Fatal("start added a key the person had removed")
	}
	if !strings.Contains(out, dialogAction) {
		t.Fatalf("does not say what to bind:\n%s", out)
	}
}

// With no Herdr desktop app, start still starts, and says how to get the dialog.
func TestStartWithoutTheDesktopAppSaysHowToGetIt(t *testing.T) {
	home := appHomeWith(t, "")
	dir := shortStateDir(t)
	fakeTailcatServe(t, dir, nil)
	out, errs, code := runCLI(t, "start", "--state", dir)
	if code != 0 || errs != "" {
		t.Fatalf("exit %d: %s", code, errs)
	}
	for _, w := range []string{"already running", "https://github.com/herdrdev/herdr-app", "herdr-anywhere pair"} {
		if !strings.Contains(out, w) {
			t.Errorf("want %q in:\n%s", w, out)
		}
	}
	if _, err := os.Stat(filepath.Join(home, "mods")); err == nil {
		t.Fatal("made a mods folder for an app that isn't there")
	}
}

// A mod of someone else's under the same name stays; start still starts.
func TestStartLeavesSomeoneElsesMod(t *testing.T) {
	home := appHomeWith(t, someKeys)
	os.MkdirAll(modFile(home, ""), 0o755)
	os.WriteFile(modFile(home, "mod.json"), []byte(`{"place":"right"}`), 0o644)
	dir := shortStateDir(t)
	fakeTailcatServe(t, dir, nil)
	out, errs, code := runCLI(t, "start", "--state", dir)
	if code != 0 || !strings.Contains(out, "already running") {
		t.Fatalf("exit %d: %s %s", code, out, errs)
	}
	if read(t, modFile(home, "mod.json")) != `{"place":"right"}` || !strings.Contains(out+errs, modFile(home, "")) {
		t.Fatalf("replaced it, or didn't say why there is no dialog:\n%s%s", out, errs)
	}
}

// When the dialog's own Start runs start, nothing is written: Herdr is running those files.
func TestStartWritesNothingWhenTheDialogIsCurrent(t *testing.T) {
	home := appHomeWith(t, someKeys)
	dir := shortStateDir(t)
	fakeTailcatServe(t, dir, nil)
	if _, errs, code := runCLI(t, "start", "--state", dir); code != 0 {
		t.Fatal(errs)
	}
	past := time.Now().Add(-time.Hour)
	for _, f := range []string{"main.js", "logic.js", "mod.json", "config.js"} {
		os.Chtimes(modFile(home, f), past, past)
	}
	if _, errs, code := runCLI(t, "start", "--state", dir); code != 0 {
		t.Fatal(errs)
	}
	for _, f := range []string{"main.js", "logic.js", "mod.json", "config.js"} {
		if fi, _ := os.Stat(modFile(home, f)); fi.ModTime().After(past.Add(time.Minute)) {
			t.Fatalf("%s was written again", f)
		}
	}
}

// A serve that starts (at login, or as the new version after an update) brings an installed dialog up to date; it
// installs none and leaves config.js, which install-dialog or start wrote for that person.
func TestServeRefreshesAnInstalledDialog(t *testing.T) {
	home := oldDialog(t, "cmd-k")
	startServe(t, "--listen", "127.0.0.1:0", "--state", shortStateDir(t))
	if read(t, modFile(home, "main.js")) != embedded(t, "main.js") {
		t.Fatal("serve left the old dialog")
	}
	if !strings.Contains(read(t, modFile(home, "config.js")), "/old/herdr-anywhere") {
		t.Fatal("serve rewrote config.js")
	}

	empty := appHomeWith(t, someKeys)
	startServe(t, "--listen", "127.0.0.1:0", "--state", shortStateDir(t))
	if _, err := os.Stat(filepath.Join(empty, "mods")); err == nil {
		t.Fatal("serve installed a dialog nobody asked for")
	}
}

// A dialog from 0.1.19 has no strings.js. Refreshing it brings strings.js, before the main.js that imports it.
func TestRefreshingAnEnglishOnlyDialogBringsItsWords(t *testing.T) {
	home := oldDialog(t, "")
	if _, err := os.Stat(modFile(home, "strings.js")); err == nil {
		t.Fatal("the old dialog already has strings.js")
	}
	if changed, err := refreshDialog(home); err != nil || !changed {
		t.Fatalf("refresh: changed %v, %v", changed, err)
	}
	for _, f := range []string{"strings.js", "main.js"} {
		if read(t, modFile(home, f)) != embedded(t, f) {
			t.Fatalf("%s is not this version's", f)
		}
	}
	if dialogFileNames[0] != "strings.js" || dialogFileNames[len(dialogFileNames)-1] != "main.js" {
		t.Fatalf("written in the order %v: main.js would import a strings.js that isn't there yet", dialogFileNames)
	}
}

package cursor

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"herdr-anywhere/companion/internal/chat"
)

func requireSupported(t *testing.T) {
	t.Helper()
	if !supported() {
		t.Skip("Cursor chat is unavailable on this platform")
	}
}

func TestUnsupportedPlatform(t *testing.T) {
	if supportsOS("windows") || !supportsOS("linux") || !supportsOS("darwin") {
		t.Fatal("unsupported platform offered")
	}
	old := supported
	supported = func() bool { return false }
	t.Cleanup(func() { supported = old })
	s := &Setup{Dir: t.TempDir(), Binary: "/companion", HC: "/hc"}
	if s.State() != chat.NoAgent {
		t.Fatal("setup offered on unsupported platform")
	}
	if !errors.Is(s.Install(false), chat.ErrNoAgent) {
		t.Fatal("installed unsupported setup")
	}
	if _, err := os.Stat(s.File()); !os.IsNotExist(err) {
		t.Fatal("wrote unsupported hook", err)
	}
	r := &Reader{Setup: s}
	if _, _, err := r.FindPane(chat.Pane{Pid: os.Getpid()}); err == nil {
		t.Fatal("offered unsupported chat")
	}
	if _, ok := r.UnreportedPane(os.Getpid(), nil); ok {
		t.Fatal("discovered unsupported chat")
	}
}

func TestEditedManagedHookNeedsForce(t *testing.T) {
	requireSupported(t)
	for _, field := range []string{"matcher", "timeout", "command"} {
		for _, install := range []bool{false, true} {
			t.Run(fmt.Sprintf("%s/install=%v", field, install), func(t *testing.T) {
				s := &Setup{Dir: t.TempDir(), Binary: filepath.Join(t.TempDir(), "companion"), HC: filepath.Join(t.TempDir(), "hc")}
				if err := s.Install(false); err != nil {
					t.Fatal(err)
				}
				b, _ := os.ReadFile(s.File())
				var d map[string]any
				_ = json.Unmarshal(b, &d)
				entry := d["hooks"].(map[string]any)["sessionStart"].([]any)[0].(map[string]any)
				entry[field] = "user-edited"
				if field == "timeout" {
					entry[field] = 19
				}
				b, _ = json.Marshal(d)
				if err := os.WriteFile(s.File(), b, 0600); err != nil {
					t.Fatal(err)
				}
				if s.State() != chat.Changed {
					t.Fatal("edited hook treated as upgrade", s.State())
				}
				if err := s.change(install, false); !errors.Is(err, chat.ErrChanged) {
					t.Fatal("edited hook was not protected", err)
				}
				after, _ := os.ReadFile(s.File())
				if !bytes.Equal(after, b) {
					t.Fatal("changed hook without consent")
				}
				if err := s.change(install, true); err != nil {
					t.Fatal(err)
				}
				want := chat.Absent
				if install {
					want = chat.Installed
				}
				if s.State() != want {
					t.Fatal(s.State())
				}
			})
		}
	}
}

func TestSetupMergesAndRemoves(t *testing.T) {
	requireSupported(t)
	s := &Setup{Dir: t.TempDir(), Binary: filepath.Join(t.TempDir(), "Herdr Anywhere"), HC: filepath.Join(t.TempDir(), "hc")}
	original := []byte(`{"version":1,"extra":{"number":1e4,"text":" untouched "},"hooks":{"sessionStart":[{"command":"user-start","timeout":17,"matcher":"custom"}],"stop":[{"command":"user-stop"}]}}`)
	if e := os.WriteFile(s.File(), original, 0600); e != nil {
		t.Fatal(e)
	}
	if s.State() != chat.Absent {
		t.Fatal(s.State())
	}
	var before map[string]json.RawMessage
	_ = json.Unmarshal(original, &before)
	if e := s.Install(false); e != nil {
		t.Fatal(e)
	}
	if s.State() != chat.Installed {
		t.Fatal(s.State())
	}
	first, _ := os.ReadFile(s.File())
	if e := s.Install(false); e != nil {
		t.Fatal(e)
	}
	again, _ := os.ReadFile(s.File())
	if !bytes.Equal(first, again) {
		t.Fatal("not idempotent")
	}
	s.Binary = filepath.Join(t.TempDir(), "new-companion")
	if s.State() != chat.Old {
		t.Fatal(s.State())
	}
	if e := s.Install(false); e != nil {
		t.Fatal(e)
	}
	if e := s.Remove(false); e != nil {
		t.Fatal(e)
	}
	b, _ := os.ReadFile(s.File())
	var after map[string]json.RawMessage
	_ = json.Unmarshal(b, &after)
	var a, bv any
	_ = json.Unmarshal(before["extra"], &a)
	_ = json.Unmarshal(after["extra"], &bv)
	aa, _ := json.Marshal(a)
	bb, _ := json.Marshal(bv)
	if !bytes.Equal(aa, bb) {
		t.Fatal("changed user metadata")
	}
	var beforeHooks, afterHooks map[string]json.RawMessage
	_ = json.Unmarshal(before["hooks"], &beforeHooks)
	_ = json.Unmarshal(after["hooks"], &afterHooks)
	for _, event := range []string{"sessionStart", "stop"} {
		var x, y any
		_ = json.Unmarshal(beforeHooks[event], &x)
		_ = json.Unmarshal(afterHooks[event], &y)
		xx, _ := json.Marshal(x)
		yy, _ := json.Marshal(y)
		if !bytes.Equal(xx, yy) {
			t.Fatal("changed user hook", event)
		}
	}
	if _, ok := afterHooks["beforeSubmitPrompt"]; ok {
		t.Fatal("left managed hook")
	}
}

func TestSetupLeavesMalformedAndForeign(t *testing.T) {
	s := &Setup{Dir: t.TempDir(), Binary: filepath.Join(t.TempDir(), "companion"), HC: filepath.Join(t.TempDir(), "hc")}
	for _, raw := range []string{`oops`, `null`, `{"version":2,"hooks":{}}`, `{"hooks":{}}`, `{"version":1,"hooks":[]}`, `{"version":1,"hooks":{"stop":null}}`, `{"version":1,"hooks":{"stop":[{}]}}`} {
		if e := os.WriteFile(s.File(), []byte(raw), 0600); e != nil {
			t.Fatal(e)
		}
		for _, force := range []bool{false, true} {
			if e := s.Install(force); e == nil {
				t.Fatal("installed over malformed/foreign file", raw)
			}
			if e := s.Remove(force); e == nil {
				t.Fatal("removed malformed/foreign file", raw)
			}
		}
		b, _ := os.ReadFile(s.File())
		if string(b) != raw {
			t.Fatal("touched foreign file")
		}
	}
}

func TestSetupOwnFile(t *testing.T) {
	requireSupported(t)
	s := &Setup{Dir: t.TempDir(), Binary: filepath.Join(t.TempDir(), "companion"), HC: filepath.Join(t.TempDir(), "hc")}
	if e := s.Install(false); e != nil {
		t.Fatal(e)
	}
	if e := s.Remove(false); e != nil {
		t.Fatal(e)
	}
	if _, e := os.Stat(s.File()); !os.IsNotExist(e) {
		t.Fatal("left file", e)
	}
	s.Dir = filepath.Join(s.Dir, "absent")
	if s.State() != chat.NoAgent {
		t.Fatal(s.State())
	}
	if e := s.Install(false); e != chat.ErrNoAgent {
		t.Fatal(e)
	}
}

func TestHooksHomeAndDatabaseOverride(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	t.Setenv("USERPROFILE", home)
	r, path, _ := fixture(t, "store.db")
	t.Setenv("CURSOR_CONFIG_DIR", r.Dir)
	s := &Setup{Binary: filepath.Join(home, "companion"), HC: filepath.Join(home, "hc")}
	if s.State() != chat.Absent {
		t.Fatal("override agent was not detected", s.State())
	}
	if err := s.Install(false); err != nil {
		t.Fatal(err)
	}
	if s.File() != filepath.Join(home, ".cursor", "hooks.json") {
		t.Fatal("hooks followed DB override", s.File())
	}
	reader := &Reader{}
	cwd := filepath.Join(string(filepath.Separator), "home", "cedar", "paper-garden")
	p := chat.Pane{Pid: 42, Labels: map[string]string{chat.LabelPid: "42", chat.LabelSession: fixtureID, LabelCwd: cwd}}
	got, _, err := reader.FindPane(p)
	if err != nil || got != path {
		t.Fatalf("database did not follow override: %q %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(r.Dir, "hooks.json")); !os.IsNotExist(err) {
		t.Fatal("hooks written to DB root")
	}
}

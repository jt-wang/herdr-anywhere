package main

import (
	"bytes"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"testing"
)

// helpCommands is the word after the prefix on each usage line of the help.
func helpCommands(t *testing.T, args []string, prefix ...string) []string {
	t.Helper()
	var out, errb bytes.Buffer
	run(args, &out, &errb)
	seen := map[string]bool{}
	for _, line := range strings.Split(out.String()+errb.String(), "\n") {
		f := strings.Fields(line)
		if len(f) > len(prefix) && reflect.DeepEqual(f[:len(prefix)], prefix) {
			seen[f[len(prefix)]] = true
		}
	}
	var names []string
	for n := range seen {
		names = append(names, n)
	}
	sort.Strings(names)
	return names
}

// The Companion's commands are exactly these: a command added or brought back
// shows up here first. Each one listed also runs (it isn't an unknown command).
func TestTheCompanionHasExactlyTheseCommands(t *testing.T) {
	want := []string{"chat-setup", "devices", "install-dialog", "language", "pair", "progress", "revoke", "serve", "start", "stop", "track", "update"}
	if got := helpCommands(t, []string{"--help"}, "herdr-anywhere"); !reflect.DeepEqual(got, want) {
		t.Fatalf("herdr-anywhere --help lists %v, want %v", got, want)
	}
	for _, c := range want {
		var out, errb bytes.Buffer
		run([]string{c, "--help"}, &out, &errb)
		if strings.Contains(errb.String(), "unknown command") {
			t.Errorf("%s is listed but unknown: %s", c, errb.String())
		}
	}
}

// track's subcommands are exactly these. None of them types into another pane.
func TestTrackHasExactlyTheseSubcommands(t *testing.T) {
	want := []string{"add", "file", "move", "place", "queue", "report", "scan", "set", "status", "tree", "watch"}
	if got := helpCommands(t, []string{"track", "--help"}, "herdr-anywhere", "track"); !reflect.DeepEqual(got, want) {
		t.Fatalf("herdr-anywhere track --help lists %v, want %v", got, want)
	}
}

// A command's help, or a word it does not know, writes nothing: not even the state folder. progress --help made an
// empty ~/.config/herdr-anywhere, in the real home when the test ran with it.
func TestHelpLeavesTheStateFolderAlone(t *testing.T) {
	t.Setenv("HERDR_ANYWHERE_STATE", "")
	for _, args := range [][]string{{"--help"}, {"progress"}, {"progress", "--help"}, {"progress", "nope"},
		{"chat-setup", "--help"}, {"devices", "--help"}, {"install-dialog", "--help"}, {"language", "--help"},
		{"pair", "--help"}, {"revoke", "--help"}, {"serve", "--help"}, {"start", "--help"}, {"stop", "--help"},
		{"track", "--help"}, {"update", "--help"}} {
		home := t.TempDir()
		setHome(t, home)
		var out, errb bytes.Buffer
		run(args, &out, &errb)
		if _, err := os.Stat(filepath.Join(home, ".config", "herdr-anywhere")); err == nil {
			t.Errorf("herdr-anywhere %s made the state folder", strings.Join(args, " "))
		}
	}
}

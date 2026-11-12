package main

import (
	"bytes"
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

package main

import (
	"herdr-anywhere/companion/internal/testfake"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"
)

// The progress page ships experimental and off by default. While
// it is off, serve does what it did before the page existed: the progress
// loop asks herdr-core nothing (no pane list, no filing pane, no typing), so
// no model runs, and it writes nothing of its own into the state dir.
func TestWhileTheProgressPageIsOffServeTouchesNothingOfIt(t *testing.T) {
	dir := shortStateDir(t)
	d := t.TempDir()
	hc := testfake.HC(t, d, "hc", testfake.Spec{Version: "hc 0.1.18", Calls: filepath.Join(d, "calls"), On: map[string][]testfake.Reply{
		"ls": {{Print: `{"terminals":[{"address":"t1","name":"api","state":"live","status":"done","turn":3,"agent":"claude-code"}]}`}},
	}})
	_, stderr := startServe(t, "--listen", "127.0.0.1:0", "--state", dir, "--hc", hc)
	time.Sleep(1500 * time.Millisecond) // the loop's first pass runs at start

	b, _ := os.ReadFile(filepath.Join(d, "calls"))
	for _, c := range strings.Split(strings.TrimSpace(string(b)), "\n") {
		if c != "" && c != "--version" {
			t.Errorf("hc %s while the page is off", c)
		}
	}
	var names []string
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		names = append(names, e.Name())
	}
	sort.Strings(names)
	for _, n := range names {
		switch n {
		case "track.json", "watch.json", "organizer.json", "progress.json", "organizer":
			t.Errorf("%s in the state dir while the page is off (all: %v)", n, names)
		}
	}
	if s := stderr.String(); strings.Contains(s, "progress:") {
		t.Errorf("the progress loop ran: %s", s)
	}
}

// Turning the page off closes the filing pane, so no agent of it stays open
// on the computer.
func TestTurningTheProgressPageOffClosesTheFilingPane(t *testing.T) {
	state := t.TempDir()
	t.Setenv("HERDR_ANYWHERE_STATE", state)
	d := t.TempDir()
	hc := testfake.HC(t, d, "hc", testfake.Spec{Version: "hc 0.1.18", On: map[string][]testfake.Reply{
		"ls":   {{Print: `{"terminals":[{"address":"t8","name":"herdr-organizer","state":"live","status":"idle","turn":1,"agent":"claude-code"}]}`}},
		"get":  {{Print: `{"address":"t8","state":"live","labels":{"herdr.role":"organizer"},"status":{"state":"idle"}}`}},
		"kill": {{Append: filepath.Join(d, "killed")}},
	}})
	var out strings.Builder
	if err := progressCommand([]string{"on"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if err := progressCommand([]string{"off", "--hc", hc}, &out, &out); err != nil {
		t.Fatalf("%v %s", err, out.String())
	}
	if progressOn(state) {
		t.Fatal("still on")
	}
	if b, _ := os.ReadFile(filepath.Join(d, "killed")); strings.TrimSpace(string(b)) != "kill t8" {
		t.Fatalf("killed: %q", b)
	}
	// Off with no filing pane, or no hc at all, still turns it off.
	os.Remove(filepath.Join(d, "killed"))
	progressCommand([]string{"on"}, &out, &out)
	if err := progressCommand([]string{"off", "--hc", filepath.Join(d, "nope")}, &out, &out); err != nil || progressOn(state) {
		t.Fatalf("off without hc: %v, on=%v", err, progressOn(state))
	}
}

// The filing agent writes what the user reads (titles, what a pane asks) in the
// phone's language, which the phone tells the companion: when the user picks the
// agent, and again when the phone's language changes. Names, file names and
// quoted words stay as they are.
func TestTheFilingAgentWritesInThePhonesLanguage(t *testing.T) {
	state := t.TempDir()
	t.Setenv("HERDR_ANYWHERE_STATE", state)
	defer func(f func() map[string]string) { agentPaths = f }(agentPaths)
	agentPaths = func() map[string]string { return map[string]string{"pi": "/x/pi"} }
	var out strings.Builder
	if err := progressCommand([]string{"organizer", "--agent", "pi", "--lang", "ja"}, &out, &out); err != nil {
		t.Fatalf("%v %s", err, out.String())
	}
	if cfg := loadOrganizer(state); cfg.Lang != "ja" {
		t.Fatalf("lang = %q", cfg.Lang)
	}
	if k := organizerKickFor(loadOrganizer(state)); !strings.Contains(k, "Japanese") || !strings.Contains(k, "as they are") {
		t.Fatalf("kick: %s", k)
	}
	if err := progressCommand([]string{"lang", "zh-Hant"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	if k := organizerKickFor(loadOrganizer(state)); !strings.Contains(k, "Traditional Chinese") {
		t.Fatalf("kick after the phone changed language: %s", k)
	}
	if loadOrganizer(state).Agent != "pi" {
		t.Fatal("changing the language lost the agent")
	}
	if err := progressCommand([]string{"lang", "x y"}, &out, &out); err == nil {
		t.Fatal("took a language tag with a space")
	}
	// Before any phone said: the language the user wrote in.
	if k := organizerKickFor(defaultOrganizer(state)); !strings.Contains(k, "language the user wrote") {
		t.Fatalf("kick with no language: %s", k)
	}
}

// The filing agent answers a batch with one JSON document in a quoted heredoc,
// so the shell keeps "$29" or a `name` as written. In the evaluation, a
// double-quoted --waiting turned "is the price really $29?" into "is the price
// really ?" (companion/eval/filing, rule R4).
func TestTheFilingRulesAnswerInOneQuotedBatch(t *testing.T) {
	for _, want := range []string{"herdr-anywhere track place --batch - <<'END'", `"places": [`, "\n    END\n"} {
		if !strings.Contains(organizerRules, want) {
			t.Errorf("filing rules lack %q", want)
		}
	}
	for _, not := range []string{"--waiting", "--title", "--more", "--task"} {
		if strings.Contains(organizerRules, not) {
			t.Errorf("filing rules still have %s", not)
		}
	}
}

// Agents that run commands without asking (Pi) looked around the computer to
// find the progress file (companion/eval/filing, rule R6); the rules say the
// queue's output is all it needs.
func TestTheFilingRulesSayTheQueueIsAllItNeeds(t *testing.T) {
	if !strings.Contains(organizerRules, "Run no other command") || !strings.Contains(organizerRules, "do not look for files") {
		t.Errorf("filing rules lack the line against looking around")
	}
}

// When the user tells the filing pane itself that a task's stage or place is
// wrong ("version 2.1 shipped weeks ago"), it can put it right: its rules say how
// and Claude Code may run those two commands without asking.
//
// promptSuggestionEnabled is the setting name in Claude Code 2.1.325's program ("When false, prompt suggestions
// are disabled"). In the filing eval on Claude Code 2.1.325 with Haiku 5.5, seven languages run with it and seven
// without, the input box was empty before the correction every time (no suggestion either way), so what it
// changes is not shown yet; organizerKick's screen check does not rely on it. Re-check: companion/eval/filing/run.sh
// keeps each language's before-correction.txt.
func TestTheFilingPaneCanApplyWhatTheUserCorrects(t *testing.T) {
	for _, want := range []string{"Bash(herdr-anywhere track set:*)", "Bash(herdr-anywhere track move:*)", `"promptSuggestionEnabled": false`} {
		if !strings.Contains(organizerSettings, want) {
			t.Errorf("settings lack %s", want)
		}
	}
	for _, want := range []string{"herdr-anywhere track set <id> --stage", "herdr-anywhere track move <id>"} {
		if !strings.Contains(organizerRules, want) {
			t.Errorf("filing rules lack %q", want)
		}
	}
}

// A scan's tasks are judged one by one from what the agent answered to each,
// and tasks from several panes that serve one bigger outcome go under a goal.
func TestTheFilingRulesJudgeEachScannedTaskAndGroupThem(t *testing.T) {
	for _, want := range []string{"Agent:", `"goals": [`, `"key": "G1"`, "keeps its T id, never a new task", "to one already in tasks to put it under"} {
		if !strings.Contains(organizerRules, want) {
			t.Errorf("filing rules lack %q", want)
		}
	}
}

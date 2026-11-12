package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"herdr-anywhere/companion/internal/hcbin"
	"herdr-anywhere/companion/internal/track"
	"herdr-anywhere/companion/internal/watch"
)

// The filing pane: an agent in its own herdr-core terminal (label
// herdr.role=organizer) that sorts queued messages and scanned sessions into
// tasks. The companion never calls a model itself:
// whatever needs judgement is an agent's job, in a pane the user
// can see and stop, with whatever harness and model they configured.

const organizerRole = "organizer"

type organizerConfig struct {
	Cmd   string        `json:"cmd"`             // the agent's command line
	Agent string        `json:"agent,omitempty"` // the agent the user picked (progress agents), "" before
	Lang  string        `json:"lang,omitempty"`  // the phone's language (BCP 47), what the agent writes in; "" before a phone said
	Dir   string        `json:"dir"`             // its folder: rules, settings
	Name  string        `json:"name"`            // its terminal name
	Batch int           `json:"batch"`           // kick it once this many items wait…
	Wait  time.Duration `json:"wait"`            // …or the oldest has waited this long
}

func defaultOrganizer(state string) organizerConfig {
	return organizerConfig{Cmd: "claude --model claude-haiku-5-5", Dir: filepath.Join(state, "organizer"), Name: "herdr-organizer", Batch: 5, Wait: 3 * time.Minute}
}

func saveOrganizer(state string, cfg organizerConfig) error {
	b, _ := json.MarshalIndent(cfg, "", "  ")
	return os.WriteFile(filepath.Join(state, "organizer.json"), append(b, '\n'), 0o600)
}

func loadOrganizer(state string) organizerConfig {
	cfg := defaultOrganizer(state)
	if b, err := os.ReadFile(filepath.Join(state, "organizer.json")); err == nil {
		json.Unmarshal(b, &cfg)
	}
	return cfg
}

// langNames are the app's languages by name, for the filing agent.
var langNames = map[string]string{"en": "English", "zh-Hans": "Simplified Chinese", "zh-Hant": "Traditional Chinese",
	"es": "Spanish", "ja": "Japanese", "ko": "Korean", "ru": "Russian"}

var langTag = regexp.MustCompile(`^[A-Za-z]{2,3}(-[A-Za-z0-9]{2,8})*$`)

// organizerKickFor is what the companion types into the filing pane for a batch.
// It says everything needed, so the pane needs no memory of earlier batches,
// and names the language the user reads the page in.
func organizerKickFor(cfg organizerConfig) string {
	lang := "the language the user wrote in"
	if n := langNames[cfg.Lang]; n != "" {
		lang = n
	} else if cfg.Lang != "" {
		lang = "the language with BCP 47 tag " + cfg.Lang
	}
	return "Filing queue: run herdr-anywhere track queue --json, then answer every item in one herdr-anywhere track place --batch - command, " +
		"as the rules in this folder's CLAUDE.md (the same as AGENTS.md) show. Write titles and waiting in " + lang +
		"; keep names, file names, commands and the user's quoted words as they are. Then reply with one line: how many you placed."
}

// organizerDue says whether a batch should go to the filing pane now.
func organizerDue(q []track.Pending, cfg organizerConfig, now time.Time) bool {
	if len(q) == 0 {
		return false
	}
	if len(q) >= cfg.Batch {
		return true
	}
	oldest := q[0].At
	for _, p := range q {
		if p.At < oldest {
			oldest = p.At
		}
	}
	// A scanned session or a stopped pane is worth sorting at once: the user is
	// waiting for the list, or to see the task finished or what it asks.
	for _, p := range q {
		if p.Kind == "session" || p.Kind == "stop" {
			return true
		}
	}
	return now.Sub(time.UnixMilli(oldest)) >= cfg.Wait
}

// organizerKick starts the filing pane if needed and, when a batch is due and the
// pane is idle or done, types the batch prompt. It returns what it did.
func organizerKick(hc, state string, st *track.Store, now time.Time) (string, error) {
	f, err := st.Load()
	if err != nil {
		return "", err
	}
	cfg := loadOrganizer(state)
	if !organizerDue(f.Queue, cfg, now) {
		return "", nil
	}
	pane := organizerPane(cfg, state)
	// Its track commands read this companion's progress file, herdr-core only.
	addr, err := rolePaneStart(hc, pane, organizerRole, organizerRulesText(), organizerSettings, "HERDR_ANYWHERE_STATE="+state, "HERDR_TRACK_HERDR=-")
	if err != nil {
		return "", err
	}
	ps, err := hcStatus(hc, addr)
	if err != nil {
		return "", err
	}
	if ps.TermState != "live" || (ps.State != "idle" && ps.State != "done") {
		return "", nil // busy, or on a dialog the user sees under Needs you
	}
	if out, err := runFor(10*time.Second, hc, "send", addr, organizerKickFor(cfg)); err != nil {
		return "", fmt.Errorf("hc send %s: %v %s", addr, err, out)
	}
	return fmt.Sprintf("handed %d to the filing pane %s", len(f.Queue), addr), nil
}

// organizerPane is the filing pane's command. Codex runs in its sandbox, where it
// may write in its own folder and the progress file's, which it is told here.
func organizerPane(cfg organizerConfig, state string) rolePane {
	pane := rolePane{Name: cfg.Name, Dir: cfg.Dir, Cmd: cfg.Cmd}
	if cfg.Agent == "codex" {
		pane.Extra = []string{"--add-dir", state}
	}
	return pane
}

// organizerSettings is a project settings file for Claude Code in the pane's
// folder: it may run the track commands its rules name without asking, nothing else.
// Other harnesses ask the user, which shows up as the pane waiting.
const organizerSettings = `{
  "permissions": {
    "allow": ["Bash(herdr-anywhere track queue:*)", "Bash(herdr-anywhere track place:*)", "Bash(herdr-anywhere track tree:*)",
      "Bash(herdr-anywhere track set:*)", "Bash(herdr-anywhere track move:*)"]
  }
}
`

func organizerRulesText() string {
	self, _ := os.Executable()
	return organizerRules + "\nIf `herdr-anywhere` is not on PATH, use `" + self + "` (quoted if the path has spaces).\n"
}

// organizerRules is the filing pane's CLAUDE.md and AGENTS.md, in English like
// the rest of the companion; the batch message names the language to write in.
// Task names say what gets done, in the user's words.
const organizerRules = `# You are Herdr's filing pane

You file what the user says in their panes under their tasks, and when a pane stops, decide what became of its task. When the user talks to you here, you put right what they correct. You do no project work, send nothing to other panes, and change no files.

## Each batch: two commands

A batch is a message that starts with "Filing queue:".

1. Run ` + "`herdr-anywhere track queue --json`" + `. items are what to file; tasks are all of the user's open tasks (under is the task it sits under; notes are the user's past corrections, "this kind of thing does not belong here").
2. Answer every item in one command, a JSON document between two END lines (the quotes around the first END keep the text exactly as written):

    herdr-anywhere track place --batch - <<'END'
    {"goals": [{"key": "G1", "title": "Release version 2.0", "under": "P1"}],
     "places": [
      {"id": "Q1", "stage": "done"},
      {"id": "Q2", "stage": "waiting", "waiting": "Keep the Pro plan at $12 a month?"},
      {"id": "Q3", "stage": "doing"},
      {"id": "Q4", "tasks": [{"title": "Add dark mode to Settings", "under": "P1", "stage": "done"}, {"title": "Write the 2.0 release notes", "under": "G1", "stage": "waiting", "waiting": "Mention the new prices?"}]},
      {"id": "Q5", "tasks": []},
      {"id": "Q6", "task": "T2"},
      {"id": "Q7", "task": "new", "title": "Fix the login bug before 2.0", "under": "G1"}
    ]}
    END

It prints how many it placed. If it names items it could not place, or items not placed yet, fix just those and answer again the same way. Then reply with one line: how many you placed.

For a batch, what the queue prints is all you need. Run no other command: do not look for files, read the environment or parse the output with a script.

## Language

The batch message names the language the user reads the page in. Write every title and waiting in it, even when the user or the agent wrote in another language. Keep names, file names, commands, product names, numbers, prices and the user's quoted words as they are ($12 stays $12).

## A pane stopped (kind stop)

task is the task this pane works on; text is the last thing its agent said when it stopped. From that, decide where the task is:

- Finished (it says it is changed, tested, delivered, and asks the user nothing): ` + "`\"stage\": \"done\"`" + `.
- It asks the user something, or needs the user to decide or do something to go on: ` + "`\"stage\": \"waiting\"`" + `, with ` + "`\"waiting\"`" + `: what it asks, in one sentence.
- Not finished (half done, an error, waiting on something else): ` + "`\"stage\": \"doing\"`" + `.
- "Done; shall I also do X?": finished, and it asks the user too, so waiting.

## A scanned session (kind session)

text is a pane's current session: each thing the user said (User:), oldest first, with what the agent answered to it (Agent:). List the things the user wants done in it in ` + "`\"tasks\"`" + `, one entry per thing; ` + "`\"tasks\": []`" + ` when there are none (small talk, questions about the agent).

- A thing is one outcome: a test, docs or a fix for it belong to it, not to a new one.
- Each entry: ` + "`\"title\"`" + ` and ` + "`\"under\"`" + ` for a new task, or ` + "`\"task\": \"<T id>\"`" + ` for one already in tasks. A session may have been sorted before: what is already in tasks keeps its T id, never a new task. Add ` + "`\"under\"`" + ` to one already in tasks to put it under the task or goal it belongs to.
- Give each entry the stage the session shows for that thing, read from what the agent answered to it and to everything the user said after it:
  - ` + "`\"stage\": \"done\"`" + ` when an answer says it is finished, delivered or released, and nothing later reopens it. Most things early in a long session are finished: the user went on to the next one once it was.
  - ` + "`\"stage\": \"waiting\"`" + `, with ` + "`\"waiting\"`" + `, when the latest answer about it asks the user something or needs the user to act.
  - ` + "`\"stage\": \"doing\"`" + ` when it is not finished: no answer yet, the answer says it is still going, or it failed.

## A message (kind empty)

text is what the user said in a pane; agent_said is the last thing that pane's agent said before it; pane is which pane.

- It goes on with an existing task (answers the agent, adds to it, corrects it): ` + "`\"task\": \"<T id>\"`" + `. Whatever task agent_said is about, a short reply belongs to it.
- About several existing tasks: ` + "`\"task\": \"<T id>\", \"also\": [\"<T2>\"]`" + `.
- A new task: ` + "`\"task\": \"new\", \"title\": \"<title>\", \"under\": \"<parent task id>\"`" + `.
- Not sure: put it on the most likely task and add ` + "`\"why\": \"<one reason>\"`" + `. The user does not look again, so what you place counts; prefer an existing task to a new one.
- Small talk unrelated to any task: put it on the task the pane worked on last.

## Goals

The user reads the page as a few bigger outcomes, each with the tasks that serve it. Before you answer, look at the whole batch and the tasks together:

- Tasks that serve an existing task go under it.
- When two or more tasks, new or already in tasks, from any items, serve one bigger outcome no task names yet (a release, a feature, a launch), make it a goal in ` + "`\"goals\"`" + `: a ` + "`\"key\"`" + ` (G1, G2, …), a ` + "`\"title\"`" + ` and, if it serves an existing task, ` + "`\"under\"`" + `. Then put those tasks under its key. A goal is made only when something goes under it.
- Do not make a goal for one task, or one that only repeats the task's title.

## Titles

- Say what gets done, in the user's own terms, e.g. "Add dark mode to the settings page", "Move the docs site to the new domain".
- No step numbers, module or code names, and no empty words like "step one", "fix", "improve".
- Short: about 8 words in English, about 15 characters in Chinese or Japanese; keep the technical terms needed.

## The user talks to you

Anything the user types here that is not a batch is the user correcting the page, e.g. "version 2.1 shipped weeks ago", "this belongs under the launch".

1. Run ` + "`herdr-anywhere track tree`" + ` to find the task they mean.
2. Put it right with one of:
   - ` + "`herdr-anywhere track set <id> --stage done`" + ` (or doing, parked, dropped) for a stage that is wrong;
   - ` + "`herdr-anywhere track move <id> <parent id>`" + ` (- for the top) for a task in the wrong place.
3. Reply with one line saying what you changed. If you can't tell which task they mean, ask in one line and change nothing.
`

// rolePaneStart starts a labelled helper pane (the filing pane) if no
// live one carries the label. Its rules and settings are this companion's:
// written on every call, so an update reaches a pane that already runs.
// It runs with this binary's folder on PATH so the rules can say herdr-anywhere.
func rolePaneStart(hc string, cfg rolePane, role, rules, settings string, env ...string) (string, error) {
	if err := os.MkdirAll(filepath.Join(cfg.Dir, ".claude"), 0o700); err != nil {
		return "", err
	}
	for name, text := range map[string]string{"CLAUDE.md": rules, "AGENTS.md": rules, filepath.Join(".claude", "settings.json"): settings} {
		p := filepath.Join(cfg.Dir, name)
		if text == "" {
			continue
		}
		if b, err := os.ReadFile(p); err == nil && string(b) == text {
			continue
		}
		if err := os.WriteFile(p, []byte(text), 0o600); err != nil {
			return "", err
		}
	}
	if t, err := roleFind(hc, cfg.Name, role); err != nil || t != nil {
		if t != nil {
			return t.Address, nil
		}
		return "", err
	}
	argv, err := cfg.argv()
	if err != nil {
		return "", err
	}
	self, _ := os.Executable()
	env = append([]string{"env", "PATH=" + filepath.Dir(self) + string(os.PathListSeparator) + os.Getenv("PATH")}, env...)
	args := append([]string{"run", "--bg", "--outlive-parent", "--no-parent", "--json", "--name", cfg.Name,
		"--label", roleLabel + "=" + role, "--cwd", cfg.Dir}, append(env, argv...)...)
	out, err := runFor(30*time.Second, hc, args...)
	if err != nil {
		return "", fmt.Errorf("hc run: %v %s", err, strings.TrimSpace(string(out)))
	}
	var r struct {
		Address string `json:"address"`
	}
	if json.Unmarshal(out, &r) != nil || r.Address == "" {
		return "", fmt.Errorf("hc run: unexpected output %q", strings.TrimSpace(string(out)))
	}
	return r.Address, nil
}

// organizerWaits is the filing pane when it is up but reports no status: it
// won't take a batch until someone answers whatever it shows.
func organizerWaits(hcFlag, state string) *track.OrganizerWait {
	home, _ := os.UserHomeDir()
	hc, _, err := hcbin.Resolve(hcFlag, exec.LookPath, home)
	if err != nil {
		return nil
	}
	t, err := roleFind(hc, loadOrganizer(state).Name, organizerRole)
	if err != nil || t == nil {
		return nil
	}
	if ps, err := hcStatus(hc, t.Address); err != nil || ps.State != "" {
		return nil
	}
	return &track.OrganizerWait{Addr: t.Address}
}

// roleFind is the live terminal named name carrying herdr.role=role.
func roleFind(hc, name, role string) (*hcLsTerm, error) {
	out, err := runFor(5*time.Second, hc, "ls", "--json")
	if err != nil {
		return nil, fmt.Errorf("hc ls: %w", err)
	}
	var ls struct {
		Terminals []hcLsTerm `json:"terminals"`
	}
	if err := json.Unmarshal(out, &ls); err != nil {
		return nil, fmt.Errorf("hc ls: %w", err)
	}
	for _, t := range ls.Terminals {
		if t.Name != name || t.State != "live" {
			continue
		}
		ps, err := hcStatus(hc, t.Address)
		if err != nil {
			return nil, err
		}
		if ps.Labels[roleLabel] != role {
			return nil, fmt.Errorf("terminal %s is named %s but isn't Herdr's (no %s=%s label)", t.Address, name, roleLabel, role)
		}
		t := t
		return &t, nil
	}
	return nil, nil
}

// scanItems sums up each pane's current session for the filing pane: each
// thing the person typed (the latest 30) with what the agent answered to it,
// both cut short, so it can tell what was finished from what was not.
func scanItems(sessions map[string]string) []track.SessionItem {
	var items []track.SessionItem
	for addr, path := range sessions {
		turns, err := watch.ReadSession(path)
		if err != nil || len(turns) == 0 {
			continue
		}
		if len(turns) > 30 {
			turns = turns[len(turns)-30:]
		}
		var b strings.Builder
		b.WriteString("What the user said in this pane's current session, oldest first, each with the agent's answer (long ones cut short; no Agent line: it had not answered):\n")
		for _, t := range turns {
			fmt.Fprintf(&b, "- User: %s\n", short(t.Typed, 160))
			if t.Reply != "" {
				fmt.Fprintf(&b, "  Agent: %s\n", short(t.Reply, 200))
			}
		}
		sid := strings.TrimSuffix(filepath.Base(path), ".jsonl")
		items = append(items, track.SessionItem{Pane: track.PaneRef{Side: "hc", Addr: addr}, Session: sid, Text: b.String()})
	}
	return items
}

func short(s string, n int) string {
	s = strings.Join(strings.Fields(s), " ")
	if r := []rune(s); len(r) > n {
		return string(r[:n]) + "…"
	}
	return s
}

// progressTick is one pass of the progress loop the companion runs: file what
// was typed, then hand a due batch to the filing pane. Off unless the user
// turned the progress page on (<state>/progress.json {"on": true}).
func progressTick(w *watch.Watcher, hc, state string, st *track.Store, log io.Writer) {
	if !progressOn(state) {
		return
	}
	// Turning the page on is the yes to sorting what the open panes hold already;
	// after an update that gives a scan more to go on, they are sorted once more.
	if f, err := st.Load(); err == nil && (f.ScanAnswer == "" || f.ScanAnswer == "yes" && f.ScanFormat < track.ScanFormat) {
		if ids, _, err := scanPanes(w, hc, st, time.Now()); err != nil {
			fmt.Fprintf(log, "progress: scan: %v\n", err)
		} else {
			fmt.Fprintf(log, "progress: scanned, %d queued\n", len(ids))
		}
	}
	if err := w.Tick(); err != nil {
		fmt.Fprintf(log, "progress: watch: %v\n", err)
	}
	// A pane that stopped on a task: the filing pane says what became of it.
	live := readLive(hc, "-")
	if err := st.Update(func(f *track.File) error { f.QueueStops(live, time.Now()); return nil }); err != nil {
		fmt.Fprintf(log, "progress: stops: %v\n", err)
	}
	if did, err := organizerKick(hc, state, st, time.Now()); err != nil {
		fmt.Fprintf(log, "progress: organizer: %v\n", err)
	} else if did != "" {
		fmt.Fprintf(log, "progress: %s\n", did)
	}
}

func progressOn(state string) bool {
	b, err := os.ReadFile(filepath.Join(state, "progress.json"))
	if err != nil {
		return false
	}
	var p struct {
		On bool `json:"on"`
	}
	return json.Unmarshal(b, &p) == nil && p.On
}

// progressLoop runs the progress pass every 15 s while serve runs.
func progressLoop(ctx context.Context, hc, state string, log io.Writer) {
	w, st := progressWatcher(hc, state, log)
	t := time.NewTicker(15 * time.Second)
	defer t.Stop()
	for {
		progressTick(w, hc, state, st, log)
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

func progressWatcher(hc, state string, log io.Writer) (*watch.Watcher, *track.Store) {
	home, _ := os.UserHomeDir()
	st := track.Open(filepath.Join(state, "track.json"))
	return &watch.Watcher{Home: home, State: filepath.Join(state, "watch.json"),
		List: func() ([]watch.Term, error) { return watchTerms(hc) }, File: fileTyped(st, hc, log)}, st
}

const progressUsage = `herdr-anywhere progress on|off|status|once [--hc PATH]|agents|organizer --agent ID [--lang TAG] [--hc PATH]|lang TAG|organizer --cmd "..."

on: the companion files what you type into herdr-core panes, every 15 s while
serve runs, and hands what its rules can't tell to the filing pane (an agent in
its own pane; herdr-anywhere progress organizer --cmd "..." sets which one).
off: none of that runs and the filing pane is closed; the progress file stays as it is.
once: one pass now (while on), without serve.
agents: the agents on this computer that can do the filing (JSON), and the one picked.
organizer --agent ID: pick one of them; a filing pane running another agent is closed.
lang TAG: the phone's language (BCP 47); the agent writes titles and questions in it.
`

func progressCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 {
		fmt.Fprint(stdout, progressUsage)
		return usageError{errors.New("missing subcommand")}
	}
	switch args[0] {
	case "-h", "--help", "help":
		fmt.Fprint(stdout, progressUsage)
		return nil
	case "on", "off", "once", "status", "lang", "agents", "organizer":
	default:
		return usageError{fmt.Errorf("unknown progress subcommand %q", args[0])}
	}
	// The state folder is made only for a subcommand: help and a word it does not know write nothing.
	home, _ := os.UserHomeDir()
	state := stateDir("", os.Getenv("HERDR_ANYWHERE_STATE"), home)
	if err := os.MkdirAll(state, 0o700); err != nil {
		return err
	}
	switch args[0] {
	case "on", "off":
		fs := flag.NewFlagSet("progress "+args[0], flag.ContinueOnError)
		fs.SetOutput(stderr)
		hcFlag := fs.String("hc", "", "herdr-core's hc")
		if err := fs.Parse(args[1:]); err != nil {
			return usageError{err}
		}
		b, _ := json.Marshal(map[string]bool{"on": args[0] == "on"})
		if err := os.WriteFile(filepath.Join(state, "progress.json"), append(b, '\n'), 0o600); err != nil {
			return err
		}
		if args[0] == "off" {
			// No agent of the page stays open on the computer. Without hc there is no pane to close.
			if hc, _, err := hcbin.Resolve(*hcFlag, exec.LookPath, home); err == nil {
				if t, err := roleFind(hc, loadOrganizer(state).Name, organizerRole); err == nil && t != nil {
					if out, err := runFor(10*time.Second, hc, "kill", t.Address); err != nil {
						return fmt.Errorf("hc kill %s: %v %s", t.Address, err, out)
					}
				}
			}
		}
		return nil
	case "once":
		fs := flag.NewFlagSet("progress once", flag.ContinueOnError)
		fs.SetOutput(stderr)
		hcFlag := fs.String("hc", "", "herdr-core's hc")
		if err := fs.Parse(args[1:]); err != nil {
			return usageError{err}
		}
		hc, _, err := hcbin.Resolve(*hcFlag, exec.LookPath, home)
		if err != nil {
			return err
		}
		w, st := progressWatcher(hc, state, stdout)
		progressTick(w, hc, state, st, stdout)
		return nil
	case "status":
		cfg := loadOrganizer(state)
		return writeJSON(stdout, map[string]any{"on": progressOn(state), "organizer": cfg})
	case "lang":
		if len(args) != 2 || !langTag.MatchString(args[1]) {
			return usageError{errors.New("progress lang TAG: the phone's language, as a BCP 47 tag")}
		}
		cfg := loadOrganizer(state)
		cfg.Lang = args[1]
		return saveOrganizer(state, cfg)
	case "agents":
		return writeJSON(stdout, map[string]any{"agents": organizerAgents(), "chosen": loadOrganizer(state).Agent})
	case "organizer":
		if len(args) >= 3 && args[1] == "--agent" {
			fs := flag.NewFlagSet("progress organizer", flag.ContinueOnError)
			fs.SetOutput(stderr)
			hcFlag := fs.String("hc", "", "herdr-core's hc")
			lang := fs.String("lang", "", "the phone's language (BCP 47): what the agent writes in")
			if err := fs.Parse(args[3:]); err != nil {
				return usageError{err}
			}
			if *lang != "" && !langTag.MatchString(*lang) {
				return usageError{fmt.Errorf("--lang %q is not a language tag", *lang)}
			}
			cfg, err := pickOrganizer(state, args[2], *hcFlag, *lang)
			if err != nil {
				return err
			}
			return writeJSON(stdout, cfg)
		}
		cfg := loadOrganizer(state)
		if len(args) == 3 && args[1] == "--cmd" && strings.TrimSpace(args[2]) != "" {
			cfg.Cmd = args[2]
			b, _ := json.MarshalIndent(cfg, "", "  ")
			if err := os.WriteFile(filepath.Join(state, "organizer.json"), append(b, '\n'), 0o600); err != nil {
				return err
			}
		}
		return writeJSON(stdout, cfg)
	}
	return nil
}

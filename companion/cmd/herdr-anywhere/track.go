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
	"strings"
	"time"

	"herdr-anywhere/companion/internal/hcbin"
	"herdr-anywhere/companion/internal/proc"
	"herdr-anywhere/companion/internal/track"
	"herdr-anywhere/companion/internal/watch"
)

const trackUsage = `herdr-anywhere track: the user's tasks and where each stands

usage:
  herdr-anywhere track tree [--json] [--quiet-after 1h]
  herdr-anywhere track status [--json] [--quiet-after 1h]
  herdr-anywhere track add [--under P] [--stage doing] [--std TEXT] [--next TEXT] [--waiting TEXT] <id> <outcome>
  herdr-anywhere track set [--stage S] [--std TEXT] [--next TEXT] [--waiting TEXT] [--evidence X] <id>
  herdr-anywhere track move <task id> <parent id, or - for the top>
  herdr-anywhere track report [--json] [--since 24h] [<id>]
  herdr-anywhere track file [--json] --pane hc:<terminal> <what the user typed>
  herdr-anywhere track queue [--json]
  herdr-anywhere track place <Q id> --task <T id>|new|none [--under P] [--title T] [--also T2,T3] [--stage S] [--more] [--why TEXT]
  herdr-anywhere track place <Q id> --stage done|doing|waiting [--waiting QUESTION]   (a stopped pane)
  herdr-anywhere track scan [--json]
  herdr-anywhere track watch [--once] [--every 15s]

Every command takes --file (default <state dir>/track.json, or $HERDR_TRACK_FILE),
--hc PATH and --herdr PATH. Nothing here calls a model.
A task can sit under another task (--under); a project, a milestone and a task
are only places in the tree. Stages: waiting / review / doing / parked / done / dropped.
A task with tasks under it shows its most urgent one unless its stage is set.
file and watch place what the rules can tell (a bare "go on" or "yes" goes to the
task of the pane's last turn); the rest waits in the queue for the filing pane,
which reads it with queue and answers with place; its answer goes straight
into the tree (the user ticks nothing).
watch reads what you type straight into Claude Code panes in herdr-core (only
what a person typed; text sent with hc send is left to whoever sent it). It
starts at the end of each session's history and keeps its place in
<state dir>/watch.json. It reads ~/.claude and writes nothing there.
scan queues each live Claude Code pane's current session once, for the filing
pane to sort into tasks.
move is the user's correction: moving a task out of a top-level task writes a scope note there.
`

type trackFlags struct {
	fs                *flag.FlagSet
	file, hc, herdr   *string
	stage, next, wait *string
	evidence          *string
	under, std        *string
	task, title, also *string
	why, batch        *string
	sure, more        *bool
	auto              *bool
	pane              *string
	state             *string
	once              *bool
	every             *time.Duration
	asJSON            *bool
	quietAfter, since *time.Duration
}

func newTrackFlags(sub string, stderr io.Writer) trackFlags {
	fs := flag.NewFlagSet("herdr-anywhere track "+sub, flag.ContinueOnError)
	fs.SetOutput(stderr)
	return trackFlags{
		fs:         fs,
		file:       fs.String("file", "", "track file (default <state dir>/track.json, or $HERDR_TRACK_FILE)"),
		hc:         fs.String("hc", "", "herdr-core's hc"),
		herdr:      fs.String("herdr", os.Getenv("HERDR_TRACK_HERDR"), "old Herdr's herdr (default $HERDR_TRACK_HERDR, else herdr on PATH; -: none, its panes show unknown)"),
		stage:      fs.String("stage", "", "stage"),
		next:       fs.String("next", "", "next step and who does it"),
		wait:       fs.String("waiting", "", "the question waiting on the user"),
		evidence:   fs.String("evidence", "", "a commit, file or URL to add as evidence"),
		under:      fs.String("under", "", "the task this one goes under"),
		std:        fs.String("std", "", "when it counts as done"),
		task:       fs.String("task", "", "place: an existing task id, new, or none"),
		title:      fs.String("title", "", "place: the new task's title"),
		also:       fs.String("also", "", "place: more existing tasks the message is about, comma-separated"),
		why:        fs.String("why", "", "place: why it went there, when unsure"),
		batch:      fs.String("batch", "", "place: the whole batch as one JSON document, from this file or - for stdin"),
		sure:       fs.Bool("sure", false, "place: the filing pane is sure (kept in the record; filed either way)"),
		more:       fs.Bool("more", false, "place: a scanned session with more tasks to come; keep it queued"),
		auto:       fs.Bool("auto", false, "add: filed by a program, not by a person"),
		pane:       fs.String("pane", "", "file: the pane the message was typed into, hc:<terminal>"),
		state:      fs.String("state", "", "watch: state dir (default ~/.config/herdr-anywhere, or $HERDR_ANYWHERE_STATE)"),
		once:       fs.Bool("once", false, "watch: one pass, then exit"),
		every:      fs.Duration("every", 15*time.Second, "watch: time between passes"),
		asJSON:     fs.Bool("json", false, "JSON output"),
		quietAfter: fs.Duration("quiet-after", time.Hour, "flag a pane with no new turn this long after a hand-off"),
		since:      fs.Duration("since", 24*time.Hour, "report: how far back to read commits when nothing was handed over"),
	}
}

func trackFile(flagValue string) (string, error) {
	if flagValue != "" {
		return flagValue, nil
	}
	if v := os.Getenv("HERDR_TRACK_FILE"); v != "" {
		return v, nil
	}
	home, err := os.UserHomeDir()
	if err != nil && os.Getenv("HERDR_ANYWHERE_STATE") == "" {
		return "", err
	}
	return filepath.Join(stateDir("", os.Getenv("HERDR_ANYWHERE_STATE"), home), "track.json"), nil
}

func trackCommand(args []string, stdout, stderr io.Writer) error {
	if len(args) == 0 || args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Fprint(stdout, trackUsage)
		if len(args) == 0 {
			return usageError{errors.New("missing subcommand")}
		}
		return nil
	}
	sub := args[0]
	tf := newTrackFlags(sub, stderr)
	rest, err := parseInterleaved(tf.fs, args[1:])
	if err != nil {
		return err
	}
	path, err := trackFile(*tf.file)
	if err != nil {
		return err
	}
	st := track.Open(path)
	now := time.Now()
	switch sub {
	case "status":
		f, err := st.Load()
		if err != nil {
			return err
		}
		rows := track.Status(f, readLive(*tf.hc, *tf.herdr), now, *tf.quietAfter)
		if *tf.asJSON {
			return writeJSON(stdout, rows)
		}
		fmt.Fprint(stdout, track.Board(rows, now))
		return nil
	case "add":
		if len(rest) != 2 {
			return usageError{errors.New("add needs <id> <outcome>")}
		}
		return st.Update(func(f *track.File) error {
			return f.Add(track.Thing{ID: rest[0], Outcome: rest[1], Stage: *tf.stage, Next: *tf.next, Waiting: *tf.wait,
				Parent: *tf.under, Std: *tf.std, Auto: *tf.auto, Created: now.UnixMilli(), Updated: now.UnixMilli()})
		})
	case "set":
		if len(rest) != 1 {
			return usageError{errors.New("set needs <id>")}
		}
		return st.Update(func(f *track.File) error {
			t, err := f.Get(rest[0])
			if err != nil {
				return err
			}
			if *tf.stage != "" {
				if err := t.SetStage(*tf.stage); err != nil {
					return err
				}
			}
			if *tf.next != "" {
				t.Next = *tf.next
			}
			if *tf.wait != "" {
				t.Waiting = *tf.wait
			}
			if *tf.std != "" {
				t.Std = *tf.std
			}
			if *tf.evidence != "" {
				t.Evidence = append(t.Evidence, *tf.evidence)
			}
			t.Updated = now.UnixMilli()
			return nil
		})
	case "tree":
		f, err := st.Load()
		if err != nil {
			return err
		}
		tr := track.BuildTree(f, readLive(*tf.hc, *tf.herdr), now, *tf.quietAfter)
		if tr.Queue > 0 {
			tr.OrganizerWaits = organizerWaits(*tf.hc, filepath.Dir(path))
		}
		if *tf.asJSON {
			return writeJSON(stdout, tr)
		}
		fmt.Fprint(stdout, track.TreeText(tr, now))
		return nil
	case "move":
		if len(rest) != 2 {
			return usageError{errors.New("move needs <task id> <parent id, or - for the top>")}
		}
		under := rest[1]
		if under == "-" {
			under = ""
		}
		return st.Update(func(f *track.File) error { return f.Move(rest[0], under, now) })
	case "file":
		if len(rest) != 1 || *tf.pane == "" {
			return usageError{errors.New("file needs --pane hc:<terminal> and the text")}
		}
		ref, err := parsePaneRef(*tf.pane)
		if err != nil || ref.Side != "hc" {
			return usageError{fmt.Errorf("pane %q: write hc:<terminal>", *tf.pane)}
		}
		live := readLive(*tf.hc, "-")
		if !track.Exists(ref, live) {
			return fmt.Errorf("no pane %s", *tf.pane)
		}
		var id string
		var d track.Decision
		if err := st.Update(func(f *track.File) error {
			var err error
			msg := track.Message{Pane: ref, Text: rest[0]}
			d = track.Decide(*f, live, msg)
			id, err = f.Take(d, msg, live, now)
			return err
		}); err != nil {
			return err
		}
		if *tf.asJSON {
			return writeJSON(stdout, map[string]any{"id": id, "decision": d})
		}
		if d.Sure {
			fmt.Fprintf(stdout, "%s (%s)\n", id, d.By)
		} else {
			fmt.Fprintf(stdout, "%s queued for filing\n", id)
		}
		return nil
	case "queue":
		f, err := st.Load()
		if err != nil {
			return err
		}
		v := f.View(readLive(*tf.hc, "-"))
		if *tf.asJSON {
			return writeJSON(stdout, v)
		}
		for _, p := range v.Items {
			fmt.Fprintf(stdout, "%s  %s  %s\n", p.ID, p.Pane, short40(p.Text))
		}
		return nil
	case "place":
		if *tf.batch != "" {
			return placeBatch(st, *tf.batch, readLive(*tf.hc, "-"), now, stdout)
		}
		if len(rest) != 1 || (*tf.task == "" && *tf.stage == "") {
			return usageError{errors.New("place needs <Q id> --task <T id>|new|none, or for a stopped pane --stage")}
		}
		d := track.Decision{Task: *tf.task, Under: *tf.under, Title: *tf.title, Stage: *tf.stage, Waiting: *tf.wait, More: *tf.more, Sure: *tf.sure, Why: *tf.why, By: "organizer"}
		if *tf.also != "" {
			d.Also = strings.Split(*tf.also, ",")
		}
		live := readLive(*tf.hc, "-")
		return st.Update(func(f *track.File) error {
			id, err := f.Place(rest[0], d, live, now)
			if err == nil {
				fmt.Fprintln(stdout, id)
			}
			return err
		})
	case "scan":
		home, _ := os.UserHomeDir()
		hc, _, err := hcbin.Resolve(*tf.hc, exec.LookPath, home)
		if err != nil {
			return err
		}
		w := &watch.Watcher{Home: home, List: func() ([]watch.Term, error) { return watchTerms(hc) }}
		ids, panes, err := scanPanes(w, hc, st, now)
		if err != nil {
			return err
		}
		if *tf.asJSON {
			return writeJSON(stdout, map[string]any{"queued": ids, "panes": panes})
		}
		fmt.Fprintf(stdout, "%d panes, %d queued for filing\n", panes, len(ids))
		return nil
	case "watch":
		home, _ := os.UserHomeDir()
		hc, _, err := hcbin.Resolve(*tf.hc, exec.LookPath, home)
		if err != nil {
			return err
		}
		state := stateDir(*tf.state, os.Getenv("HERDR_ANYWHERE_STATE"), home)
		if err := os.MkdirAll(state, 0o700); err != nil {
			return err
		}
		w := &watch.Watcher{Home: home, State: filepath.Join(state, "watch.json"),
			List: func() ([]watch.Term, error) { return watchTerms(hc) },
			File: fileTyped(st, hc, stdout)}
		for {
			if err := w.Tick(); err != nil {
				fmt.Fprintf(stderr, "watch: %v\n", err)
			}
			if *tf.once {
				return nil
			}
			time.Sleep(*tf.every)
		}
	case "report":
		f, err := st.Load()
		if err != nil {
			return err
		}
		id := ""
		if len(rest) == 1 {
			id = rest[0]
		}
		home, _ := os.UserHomeDir()
		hc, _, err := hcbin.Resolve(*tf.hc, exec.LookPath, home)
		if err != nil {
			return err
		}
		reps := track.Report(f, readLive(hc, *tf.herdr), track.ReportSources{HC: hc}, now, *tf.since, id)
		if *tf.asJSON {
			return writeJSON(stdout, reps)
		}
		fmt.Fprint(stdout, track.ReportText(reps))
		return nil
	default:
		fmt.Fprint(stderr, trackUsage)
		return usageError{fmt.Errorf("unknown track subcommand %q", sub)}
	}
}

// watchTerms is herdr-core's terminals for the watcher: hc ls, plus each live
// Claude Code terminal's foreground pid from hc get (hc ls doesn't list it).
func watchTerms(hc string) ([]watch.Term, error) {
	out, err := runFor(5*time.Second, hc, "ls", "--json")
	if err != nil {
		return nil, fmt.Errorf("hc ls: %w", err)
	}
	var ls struct {
		Terminals []struct {
			Address, Agent, State string
			Turn                  int
		} `json:"terminals"`
	}
	if err := json.Unmarshal(out, &ls); err != nil {
		return nil, fmt.Errorf("hc ls: %w", err)
	}
	var terms []watch.Term
	for _, t := range ls.Terminals {
		wt := watch.Term{Address: t.Address, Agent: t.Agent, State: t.State, Turn: t.Turn}
		if t.State == "live" && t.Agent == "claude-code" {
			if g, err := runFor(5*time.Second, hc, "get", t.Address, "--json"); err == nil {
				var d struct {
					Labels     map[string]string `json:"labels"`
					Foreground struct {
						PID int `json:"pid"`
					} `json:"foreground"`
				}
				if json.Unmarshal(g, &d) == nil {
					if d.Labels[roleLabel] != "" {
						continue // Herdr's own panes: nothing there is a task to file
					}
					wt.PID = d.Foreground.PID
				}
			}
		}
		terms = append(terms, wt)
	}
	return terms, nil
}

func short40(s string) string {
	r := []rune(s)
	if len(r) > 40 {
		return string(r[:40]) + "…"
	}
	return s
}

// parseInterleaved accepts flags before, between and after the arguments
// (agents write `track set A2 --stage 做完待验收`). Arguments after "--" are kept as they are.
func parseInterleaved(fs *flag.FlagSet, args []string) ([]string, error) {
	var rest []string
	for {
		if err := parse(fs, args); err != nil {
			return nil, err
		}
		left := fs.Args()
		if len(left) == 0 {
			return rest, nil
		}
		if len(args) > len(left) && args[len(args)-len(left)-1] == "--" {
			return append(rest, left...), nil
		}
		rest = append(rest, left[0])
		args = left[1:]
	}
}

func parsePaneRef(s string) (track.PaneRef, error) {
	side, v, ok := strings.Cut(s, ":")
	if !ok || v == "" {
		return track.PaneRef{}, fmt.Errorf("pane %q: write hc:<terminal> or herdr:<pane id or name>", s)
	}
	switch side {
	case "hc":
		return track.PaneRef{Side: "hc", Addr: v}, nil
	case "herdr":
		// Old Herdr pane ids look like w1:p6.
		if strings.HasPrefix(v, "w") && strings.Contains(v, ":p") {
			return track.PaneRef{Side: "herdr", PaneID: v}, nil
		}
		return track.PaneRef{Side: "herdr", Name: v}, nil
	}
	return track.PaneRef{}, fmt.Errorf("pane %q: side must be hc or herdr", s)
}

func writeJSON(w io.Writer, v any) error {
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	return enc.Encode(v)
}

// readLive runs one "hc ls --all" and asks old Herdr for its agent list once, 5 s each.
func readLive(hcFlag, herdrFlag string) track.Live {
	var live track.Live
	home, _ := os.UserHomeDir()
	if hc, _, err := hcbin.Resolve(hcFlag, exec.LookPath, home); err == nil {
		if out, err := runFor(5*time.Second, hc, "ls", "--all", "--json"); err == nil {
			if terms, err := track.ParseHCList(out); err == nil {
				for i, t := range terms {
					// What a blocked pane asks, and what a stopped agent says it did.
					if t.State == "live" && (t.Status == "blocked" || t.Status == "done" || t.Status == "idle") && t.Agent != "" {
						if st, err := hcStatus(hc, t.Address); err == nil {
							terms[i].Msg, terms[i].StatusMs = st.Msg, st.StatusMs
						}
					}
				}
				live.HC, live.HCOK = terms, true
			}
		}
	}
	herdr := herdrFlag
	if herdr == "-" {
		return live
	}
	if herdr == "" {
		herdr, _ = exec.LookPath("herdr")
	}
	if herdr != "" {
		if out, err := runFor(5*time.Second, herdr, "agent", "list"); err == nil {
			if ags, err := track.ParseHerdrList(out); err == nil {
				live.Herdr, live.HerdrOK = ags, true
			}
		}
	}
	return live
}

type paneStatus struct {
	Address, TermState, State, Msg string
	StatusMs                       int64
	Labels                         map[string]string
}

// hcStatus reads one terminal's status from hc get: hc ls gives only the
// state word, hc get also gives what a blocked pane is asking.
func hcStatus(hc, addr string) (paneStatus, error) {
	out, err := runFor(5*time.Second, hc, "get", addr, "--json")
	if err != nil {
		return paneStatus{}, fmt.Errorf("hc get %s: %w", addr, err)
	}
	var d struct {
		Address  string            `json:"address"`
		State    string            `json:"state"`
		StatusMs int64             `json:"status_ms"`
		Labels   map[string]string `json:"labels"`
		Status   *struct {
			State string `json:"state"`
			Msg   string `json:"msg"`
		} `json:"status"`
	}
	if err := json.Unmarshal(out, &d); err != nil {
		return paneStatus{}, fmt.Errorf("hc get %s: %w", addr, err)
	}
	ps := paneStatus{Address: d.Address, TermState: d.State, StatusMs: d.StatusMs, Labels: d.Labels}
	if d.Status != nil {
		ps.State, ps.Msg = d.Status.State, d.Status.Msg
	}
	return ps, nil
}

func runFor(d time.Duration, name string, args ...string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), d)
	defer cancel()
	return proc.CommandContext(ctx, name, args...).Output()
}

// fileTyped files one message typed into a pane: by rule, or into the queue.
func fileTyped(st *track.Store, hc string, log io.Writer) func(addr string, m watch.Msg) error {
	return func(addr string, m watch.Msg) error {
		live := readLive(hc, "-")
		msg := track.Message{Pane: track.PaneRef{Side: "hc", Addr: addr}, Text: m.Text, PrevEnded: m.PrevEnded, AgentSaid: m.AgentSaid}
		var id string
		var d track.Decision
		err := st.Update(func(f *track.File) error {
			var err error
			d = track.Decide(*f, live, msg)
			id, err = f.Take(d, msg, live, time.Now())
			return err
		})
		if err == nil {
			fmt.Fprintf(log, "%s %s %q → %s %s\n", time.Now().Format("15:04:05"), addr, short40(m.Text), id, d.By)
		}
		return err
	}
}

// scanPanes queues each live Claude Code pane's current session once, for the
// filing pane, and marks the scan as done; sessions scanned with an older scan
// format are queued again.
func scanPanes(w *watch.Watcher, hc string, st *track.Store, now time.Time) (ids []string, panes int, err error) {
	sessions, err := w.Current()
	if err != nil {
		return nil, 0, err
	}
	live := readLive(hc, "-")
	err = st.Update(func(f *track.File) error {
		var err error
		if f.ScanFormat < track.ScanFormat {
			f.Scanned = nil // scanned with less to go on: once more
		}
		ids, err = f.QueueSessions(scanItems(sessions), live, now)
		f.ScanAnswer, f.ScanFormat = "yes", track.ScanFormat
		return err
	})
	return ids, len(sessions), err
}

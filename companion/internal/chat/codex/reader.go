// Package codex is Codex's reader. A running Codex keeps its session record open: a rollout file under
// CODEX_HOME/sessions/YYYY/MM/DD/ (measured with Codex 0.167.1: its running TUI held one open; CODEX_LIVE=1 go test
// -run Live re-checks).
// So the process's open files name its record. In the record, Codex writes each finished item of a turn as an
// `event_msg` of type `item_completed` (UserMessage, AgentMessage, CommandExecution, FileChange, Reasoning, ImageView,
// McpToolCall, …), between `task_started` and `task_complete` or `turn_aborted`; the reader reads those, and the
// model's own `response_item` lines only for an image a step looked at.
package codex

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"herdr-anywhere/companion/internal/chat"
)

// Reader reads the rollout files under Dir, Codex's home (CODEX_HOME, else ~/.codex).
type Reader struct {
	Dir string

	mu     sync.Mutex
	cache  map[string]parsed
	parses int // for tests

	// openFiles lists the files the processes have open; tests replace it.
	openFiles func(pids []int) ([]string, error)
}

type parsed struct {
	size  int64
	mod   time.Time
	ino   uint64
	turns []chat.Turn
}

var _ chat.Reader = (*Reader)(nil)

// Dir is where Codex keeps its sessions, as Codex itself decides: CODEX_HOME, else ~/.codex.
func Dir() string {
	if d := os.Getenv("CODEX_HOME"); d != "" {
		return d
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".codex")
}

var rollout = regexp.MustCompile(`/sessions/\d{4}/\d{2}/\d{2}/rollout-[^/]+\.jsonl$`)

// Find: the one rollout file under Dir that the process (or a process it started: npm's `codex` is a script that
// starts the real program) has open.
func (r *Reader) Find(pid int) (string, string, error) {
	pids := append([]int{pid}, descendants(pid)...)
	list := r.openFiles
	if list == nil {
		list = openFiles
	}
	names, err := list(pids)
	if err != nil {
		return "", "", &chat.Miss{Reason: chat.NoMatch}
	}
	dir, _ := filepath.EvalSymlinks(r.Dir)
	if dir == "" {
		dir = r.Dir
	}
	seen := map[string]bool{}
	var found []string
	for _, n := range names {
		n = filepath.ToSlash(n)
		if rollout.MatchString(n) && strings.HasPrefix(n, filepath.ToSlash(dir)+"/sessions/") && !seen[n] {
			seen[n] = true
			found = append(found, n)
		}
	}
	if len(found) == 0 {
		// Codex writes the file once the first prompt is sent.
		return "", "", &chat.Miss{Reason: chat.NoRecordYet}
	}
	// Helper agents' rollouts are open too (their source names a subagent): the conversation is
	// the one thread that is not a helper's.
	var roots []string
	var v string
	for _, f := range found {
		if fv, helper := meta(f); !helper {
			roots, v = append(roots, f), fv
		}
	}
	if len(roots) == 1 {
		return roots[0], v, nil
	}
	return "", "", &chat.Miss{Reason: chat.Ambiguous}
}

// descendants: the processes started by pid, and theirs.
func descendants(pid int) []int {
	if runtime.GOOS == "windows" {
		return nil
	}
	out, err := exec.Command("ps", "-A", "-o", "pid=,ppid=").Output()
	if err != nil {
		return nil
	}
	kids := map[int][]int{}
	for _, l := range strings.Split(string(out), "\n") {
		f := strings.Fields(l)
		if len(f) != 2 {
			continue
		}
		c, err1 := strconv.Atoi(f[0])
		p, err2 := strconv.Atoi(f[1])
		if err1 == nil && err2 == nil {
			kids[p] = append(kids[p], c)
		}
	}
	var all []int
	queue := []int{pid}
	for len(queue) > 0 && len(all) < 64 {
		p := queue[0]
		queue = queue[1:]
		for _, c := range kids[p] {
			all = append(all, c)
			queue = append(queue, c)
		}
	}
	return all
}

// openFiles: on Linux from /proc, elsewhere from lsof (macOS has it in /usr/sbin). Windows: none.
func openFiles(pids []int) ([]string, error) {
	switch runtime.GOOS {
	case "windows":
		return nil, fmt.Errorf("open files are not read on Windows")
	case "linux":
		var names []string
		for _, p := range pids {
			fds, _ := filepath.Glob(fmt.Sprintf("/proc/%d/fd/*", p))
			for _, fd := range fds {
				if n, err := os.Readlink(fd); err == nil {
					names = append(names, n)
				}
			}
		}
		return names, nil
	}
	var ps []string
	for _, p := range pids {
		ps = append(ps, strconv.Itoa(p))
	}
	lsof := "/usr/sbin/lsof"
	if _, err := os.Stat(lsof); err != nil {
		lsof = "lsof"
	}
	// lsof exits 1 when one of the processes has gone; what it printed still holds.
	out, _ := exec.Command(lsof, "-nP", "-Fn", "-p", strings.Join(ps, ",")).Output()
	var names []string
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, "n/") {
			names = append(names, l[1:])
		}
	}
	return names, nil
}

// version: the cli_version in the file's first line, its session_meta.
func version(path string) string {
	v, _ := meta(path)
	return v
}

// meta: the Codex version that wrote a rollout, and whether it is a helper agent's (its source names a subagent)
// rather than a conversation's own thread. From the first line, its session_meta.
func meta(path string) (version string, helper bool) {
	f, err := os.Open(path)
	if err != nil {
		return "", false
	}
	defer f.Close()
	b := make([]byte, 64<<10)
	n, _ := f.Read(b)
	first, _, _ := bytes.Cut(b[:n], []byte("\n"))
	var l struct {
		Type    string `json:"type"`
		Payload struct {
			Version string          `json:"cli_version"`
			Source  json.RawMessage `json:"source"`
		} `json:"payload"`
	}
	if json.Unmarshal(first, &l) != nil || l.Type != "session_meta" {
		return "", false
	}
	var src map[string]json.RawMessage
	helper = json.Unmarshal(l.Payload.Source, &src) == nil && src["subagent"] != nil
	return l.Payload.Version, helper
}

// --- the record ------------------------------------------------------------------------------------------------

type line struct {
	Timestamp string          `json:"timestamp"`
	Type      string          `json:"type"`
	Payload   json.RawMessage `json:"payload"`
}

type event struct {
	Type       string          `json:"type"`
	TurnID     string          `json:"turn_id"`
	Item       json.RawMessage `json:"item"`
	DurationMs *int64          `json:"duration_ms"`
	Reason     string          `json:"reason"`
	Cwd        string          `json:"cwd"`
}

// item is an item_completed's item: the fields of the kinds the reader shows.
type item struct {
	Type    string `json:"type"`
	ID      string `json:"id"`
	Content []struct {
		Type string `json:"type"`
		Text string `json:"text"`
	} `json:"content"`
	// CommandExecution
	Command         json.RawMessage `json:"command"`
	Status          string          `json:"status"`
	AggregatedOuput string          `json:"aggregated_output"`
	ExitCode        *int            `json:"exit_code"`
	// FileChange
	Changes map[string]struct {
		Type        string `json:"type"`
		UnifiedDiff string `json:"unified_diff"`
		Content     string `json:"content"`
		MovePath    string `json:"move_path"`
	} `json:"changes"`
	Stdout string `json:"stdout"`
	Stderr string `json:"stderr"`
	// Reasoning
	Summary []string `json:"summary_text"`
	// ImageView
	Path string `json:"path"`
	// McpToolCall
	Server    string          `json:"server"`
	Tool      string          `json:"tool"`
	Arguments json.RawMessage `json:"arguments"`
	Result    json.RawMessage `json:"result"`
	// WebSearch
	Query string `json:"query"`
}

func (it *item) text() string {
	var parts []string
	for _, c := range it.Content {
		if (c.Type == "text" || c.Type == "Text") && strings.TrimSpace(c.Text) != "" {
			parts = append(parts, c.Text)
		}
	}
	return strings.Join(parts, "\n\n")
}

// command: what was run, as typed: the shell's -lc argument, else the words joined.
func (it *item) command() string {
	var words []string
	if json.Unmarshal(it.Command, &words) != nil {
		var s string
		json.Unmarshal(it.Command, &s)
		return s
	}
	if len(words) == 3 && (words[1] == "-lc" || words[1] == "-c") {
		return words[2]
	}
	return strings.Join(words, " ")
}

// files: a FileChange's paths, in order.
func (it *item) files() []string {
	var ps []string
	for p := range it.Changes {
		ps = append(ps, p)
	}
	sort.Strings(ps)
	return ps
}

func rel(p, cwd string) string {
	p = strings.TrimPrefix(p, "file://")
	cwd = strings.TrimRight(strings.TrimPrefix(cwd, "file://"), "/")
	if cwd != "" && strings.HasPrefix(p, cwd+"/") {
		return p[len(cwd)+1:]
	}
	return p
}

func title(s string) string {
	return chat.Cut(strings.TrimSpace(strings.SplitN(strings.TrimSpace(s), "\n", 2)[0]), 80)
}

// counts: the lines a change adds and removes.
func counts(kind, diff, content string) (added, deleted int) {
	switch kind {
	case "add":
		return lineCount(content), 0
	case "delete":
		return 0, lineCount(content)
	}
	for _, l := range strings.Split(diff, "\n") {
		switch {
		case strings.HasPrefix(l, "+++") || strings.HasPrefix(l, "---"):
		case strings.HasPrefix(l, "+"):
			added++
		case strings.HasPrefix(l, "-"):
			deleted++
		}
	}
	return
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(s, "\n"), "\n") + 1
}

// Turns reads the record's end, again only when the file changed.
func (r *Reader) Turns(path string) ([]chat.Turn, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	ino := chat.Inode(st)
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.cache[path]; ok && c.size == st.Size() && c.mod.Equal(st.ModTime()) && c.ino == ino {
		return c.turns, nil
	}
	ts, err := r.parse(path)
	if err != nil {
		return nil, err
	}
	if r.cache == nil {
		r.cache = map[string]parsed{}
	}
	r.cache[path] = parsed{size: st.Size(), mod: st.ModTime(), ino: ino, turns: ts}
	return ts, nil
}

type building struct {
	turn    chat.Turn
	cwd     string
	pending []struct{ id, text string } // what the agent said since its last step
}

func (b *building) flushNotes() {
	for _, p := range b.pending {
		b.turn.Steps = append(b.turn.Steps, chat.Step{ID: p.id, Kind: "note", Title: title(p.text)})
	}
	b.pending = nil
}

func (b *building) step(s chat.Step) {
	b.flushNotes()
	b.turn.Steps = append(b.turn.Steps, s)
}

func (b *building) finish(end string) chat.Turn {
	var said []string
	for _, p := range b.pending {
		said = append(said, p.text)
	}
	b.turn.Reply = strings.Join(said, "\n\n")
	b.pending = nil
	b.turn.End = end
	if b.turn.Steps == nil {
		b.turn.Steps = []chat.Step{}
	}
	return b.turn
}

// parse reads the record's end. A turn the window starts in the middle of is left out.
func (r *Reader) parse(path string) ([]chat.Turn, error) {
	r.parses++
	ls, _, err := chat.TailLines(path)
	if err != nil {
		return nil, err
	}
	var turns []chat.Turn
	var b *building
	cwd := ""
	for _, raw := range ls {
		var l line
		if err := json.Unmarshal(raw, &l); err != nil {
			return nil, chat.Formatf("a line that isn't JSON")
		}
		switch l.Type {
		case "session_meta", "turn_context":
			var m struct {
				Cwd string `json:"cwd"`
			}
			if json.Unmarshal(l.Payload, &m) == nil && m.Cwd != "" {
				cwd = m.Cwd
				if b != nil {
					b.cwd = cwd
				}
			}
			continue
		case "event_msg":
		default:
			continue
		}
		var e event
		if json.Unmarshal(l.Payload, &e) != nil {
			return nil, chat.Formatf("event_msg payload")
		}
		switch e.Type {
		case "task_started":
			if b != nil { // a turn that never said it ended: a newer one started, so it stopped
				turns = append(turns, b.finish("stopped"))
			}
			b = &building{turn: chat.Turn{ID: e.TurnID, From: "you", At: l.Timestamp}, cwd: cwd}
		case "task_complete", "turn_aborted":
			if b == nil || (e.TurnID != "" && e.TurnID != b.turn.ID) {
				continue
			}
			if e.DurationMs != nil {
				b.turn.Secs = int(*e.DurationMs / 1000)
			}
			end := "done"
			if e.Type == "turn_aborted" {
				end = "stopped"
			}
			turns = append(turns, b.finish(end))
			b = nil
		case "item_completed":
			if b == nil || (e.TurnID != "" && e.TurnID != b.turn.ID) {
				continue
			}
			var it item
			if json.Unmarshal(e.Item, &it) != nil || it.Type == "" {
				return nil, chat.Formatf("item_completed item")
			}
			r.add(b, &it)
		}
	}
	if b != nil {
		turns = append(turns, b.finish("open"))
	}
	var out []chat.Turn
	for _, t := range turns {
		if t.Said != "" || len(t.Steps) > 0 || t.Reply != "" {
			out = append(out, t)
		}
	}
	if len(out) > chat.MaxTurns {
		out = out[len(out)-chat.MaxTurns:]
	}
	return out, nil
}

func (r *Reader) add(b *building, it *item) {
	switch it.Type {
	case "UserMessage":
		if t := it.text(); t != "" {
			if b.turn.Said == "" {
				b.turn.Said = t
			} else { // said again while the agent worked
				b.step(chat.Step{ID: it.ID, Kind: "note", Title: title(t)})
			}
		}
	case "AgentMessage":
		if t := it.text(); t != "" {
			b.pending = append(b.pending, struct{ id, text string }{it.ID, t})
		}
	case "CommandExecution":
		failed := it.Status == "failed" || it.Status == "declined" || (it.ExitCode != nil && *it.ExitCode != 0)
		b.step(chat.Step{ID: it.ID, Kind: "run", Title: title(it.command()), Failed: failed})
	case "FileChange":
		for i, p := range it.files() {
			c := it.Changes[p]
			a, d := counts(c.Type, c.UnifiedDiff, c.Content)
			b.step(chat.Step{ID: fmt.Sprintf("%s:%d", it.ID, i), Kind: "edit", Title: rel(p, b.cwd),
				Failed: it.Status == "failed" || it.Status == "declined", Added: a, Deleted: d})
		}
	case "Reasoning":
		if t := strings.Join(it.Summary, "\n\n"); strings.TrimSpace(t) != "" {
			b.step(chat.Step{ID: it.ID, Kind: "think", Title: title(t)})
		}
	case "ImageView":
		b.step(chat.Step{ID: it.ID, Kind: "read", Title: rel(it.Path, b.cwd)})
	case "McpToolCall":
		b.step(chat.Step{ID: it.ID, Kind: "other", Title: title(it.Tool), Failed: it.Status == "failed"})
	case "WebSearch":
		b.step(chat.Step{ID: it.ID, Kind: "web", Title: title(it.Query)})
	case "ContextCompaction", "FunctionCallOutput", "SubAgentActivity":
		// Codex's own bookkeeping.
	default:
		b.step(chat.Step{ID: it.ID, Kind: "other", Title: it.Type})
	}
}

// --- details and images ------------------------------------------------------------------------------------------

// find returns the item a step was made from, and the cwd the record had then.
func find(path, id string) (*item, string, error) {
	ls, _, err := chat.TailLines(path)
	if err != nil {
		return nil, "", err
	}
	cwd := ""
	for _, raw := range ls {
		var l line
		if json.Unmarshal(raw, &l) != nil {
			continue
		}
		if l.Type == "session_meta" || l.Type == "turn_context" {
			var m struct {
				Cwd string `json:"cwd"`
			}
			if json.Unmarshal(l.Payload, &m) == nil && m.Cwd != "" {
				cwd = m.Cwd
			}
			continue
		}
		if l.Type != "event_msg" || !bytes.Contains(raw, []byte(`"item_completed"`)) {
			continue
		}
		var e event
		var it item
		if json.Unmarshal(l.Payload, &e) == nil && e.Type == "item_completed" && json.Unmarshal(e.Item, &it) == nil && it.ID == id {
			return &it, cwd, nil
		}
	}
	return nil, "", chat.ErrNoStep
}

// Detail finds a step again: a command and what it printed, a file's change, a note's or a summary's whole text.
func (r *Reader) Detail(path, step string) (chat.Detail, error) {
	id, nth, multi := strings.Cut(step, ":")
	if multi {
		if _, err := strconv.Atoi(nth); err != nil {
			id, multi = step, false
		}
	}
	it, cwd, err := find(path, id)
	if err != nil {
		return chat.Detail{}, err
	}
	switch it.Type {
	case "CommandExecution":
		failed := it.Status == "failed" || it.Status == "declined" || (it.ExitCode != nil && *it.ExitCode != 0)
		run := &chat.Run{Command: it.command(), Failed: failed, Output: chat.CutEnd(it.AggregatedOuput, chat.MaxDetail), Lines: lineCount(it.AggregatedOuput)}
		if it.ExitCode != nil {
			run.Code = *it.ExitCode
		}
		return chat.Detail{Input: chat.Cut(run.Command, chat.MaxDetail), Output: run.Output, Run: run}, nil
	case "FileChange":
		ps := it.files()
		i, _ := strconv.Atoi(nth)
		if !multi || i < 0 || i >= len(ps) {
			return chat.Detail{}, chat.ErrNoStep
		}
		c := it.Changes[ps[i]]
		ch := change(rel(ps[i], cwd), c.Type, c.UnifiedDiff, c.Content)
		return chat.Detail{Input: rel(ps[i], cwd), Output: chat.Cut(strings.TrimSpace(it.Stdout+"\n"+it.Stderr), chat.MaxDetail), Change: ch}, nil
	case "Reasoning":
		return chat.Detail{Output: chat.Cut(strings.Join(it.Summary, "\n\n"), chat.MaxDetail)}, nil
	case "AgentMessage", "UserMessage":
		return chat.Detail{Output: chat.Cut(it.text(), chat.MaxDetail)}, nil
	case "ImageView":
		d := chat.Detail{Input: rel(it.Path, cwd)}
		if _, _, err := r.Image(path, it.ID); err == nil {
			d.Images = []chat.Image{{ID: it.ID, Source: strings.TrimPrefix(it.Path, "file://")}}
		}
		return d, nil
	case "McpToolCall":
		return chat.Detail{Input: chat.Cut(string(it.Arguments), chat.MaxDetail), Output: chat.Cut(string(it.Result), chat.MaxDetail)}, nil
	}
	return chat.Detail{Input: it.Type}, nil
}

// change: a file's change as the phone shows it, from Codex's unified diff, or a new or deleted file's text.
func change(file, kind, diff, content string) *chat.Change {
	c := &chat.Change{File: file, Hunks: []chat.Hunk{}}
	n := 0
	add := func(h *chat.Hunk, l string) bool {
		if n >= chat.MaxChangeLines {
			c.Cut = true
			return false
		}
		n++
		h.Lines = append(h.Lines, l)
		return true
	}
	switch kind {
	case "add", "delete":
		mark, h := "+", chat.Hunk{OldStart: 0, NewStart: 1}
		if kind == "delete" {
			mark, h = "-", chat.Hunk{OldStart: 1, NewStart: 0}
		}
		for _, l := range strings.Split(strings.TrimSuffix(content, "\n"), "\n") {
			if !add(&h, mark+l) {
				break
			}
		}
		c.Hunks = append(c.Hunks, h)
	default:
		c.Hunks, c.Cut = chat.DiffHunks(diff)
	}
	c.Added, c.Deleted = counts(kind, diff, content)
	return c
}

// Image finds the image a step looked at, as Codex sent it to the model: the output of the call with the step's id.
func (r *Reader) Image(path, id string) (string, []byte, error) {
	ls, _, err := chat.TailLines(path)
	if err != nil {
		return "", nil, err
	}
	for _, raw := range ls {
		if !bytes.Contains(raw, []byte(id)) || !bytes.Contains(raw, []byte(`"input_image"`)) {
			continue
		}
		var l struct {
			Type    string `json:"type"`
			Payload struct {
				Type   string          `json:"type"`
				CallID string          `json:"call_id"`
				Output json.RawMessage `json:"output"`
			} `json:"payload"`
		}
		if json.Unmarshal(raw, &l) != nil || l.Type != "response_item" || l.Payload.CallID != id {
			continue
		}
		var parts []struct {
			Type     string `json:"type"`
			ImageURL string `json:"image_url"`
		}
		json.Unmarshal(l.Payload.Output, &parts)
		for _, p := range parts {
			head, data, ok := strings.Cut(p.ImageURL, ",")
			media := strings.TrimSuffix(strings.TrimPrefix(head, "data:"), ";base64")
			if p.Type != "input_image" || !ok || !strings.HasSuffix(head, ";base64") || !chat.ImageTypes[media] {
				continue
			}
			b, err := base64.StdEncoding.DecodeString(data)
			if err != nil {
				continue
			}
			return media, b, nil
		}
	}
	return "", nil, chat.ErrNoStep
}

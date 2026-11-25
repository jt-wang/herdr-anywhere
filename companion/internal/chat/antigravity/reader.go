// Package antigravity is the Antigravity CLI's (agy) reader. Rules measured on agy 1.5.14 against a fake Gemini
// gateway (CHAT_LIVE=1 go test -run Live re-checks them):
//
// A conversation lives in brain/<conversation id>/ under agy's folder (~/.gemini/antigravity-cli); its record is
// .system_generated/logs/transcript.jsonl, one JSON object per step: step_index, source (USER_EXPLICIT, MODEL,
// SYSTEM), type, status, created_at, and the step's content, thinking, tool_calls and media. A field longer than
// agy keeps there is cut in the middle and named in truncated_fields; transcript_full.jsonl beside it has the same
// step whole.
//
// A prompt is a USER_INPUT step, its words inside <USER_REQUEST>, followed by agy's own <ADDITIONAL_METADATA> and
// <USER_SETTINGS_CHANGE>. The model's answers are PLANNER_RESPONSE steps: words, thinking, and tool calls (a name, and
// each argument's value as JSON in a string, without ids); each call's result is the next step after it (GENERIC),
// in order. A response without calls ends the turn. SYSTEM_MESSAGE steps are agy talking to the model (reminders, a
// background task's end, a subagent's report): no one's turn. A command cut off with Esc writes no result.
//
// Which conversation a running agy shows: it holds presence/<conversation id>.lock open while that conversation is
// on its screen, working or idle, and lets go of it at /clear (the folders of earlier ones stay open); a subagent's
// conversation takes no lock. Before the first prompt there is no lock.
package antigravity

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"

	"herdr-anywhere/companion/internal/chat"
)

// Reader reads under Dir, agy's own folder (Dir()); Turns, Detail and Image take a record's path and need no Dir.
type Reader struct {
	Dir string

	mu     sync.Mutex
	cache  map[string]parsed
	parses int // for tests

	// openFiles lists the files the processes have open; tests replace it.
	openFiles func(pids []int) ([]string, error)
}

type parsed struct {
	size, fullSize int64
	mod            time.Time
	ino            uint64
	turns          []chat.Turn
}

var (
	_ chat.Reader        = (*Reader)(nil)
	_ chat.HistoryReader = (*Reader)(nil)
)

// Dir is agy's folder: ~/.gemini/antigravity-cli under the home folder (agy 1.5.14 has no setting for another).
func Dir() string {
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".gemini", "antigravity-cli")
}

var conversationID = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

// Find: the one presence lock under Dir that the process (or one it started) holds open names the conversation.
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
	dir := r.Dir
	if dir == "" {
		dir = Dir()
	}
	presence := filepath.Join(dir, "presence")
	if real, err := filepath.EvalSymlinks(presence); err == nil {
		presence = real
	}
	ids := map[string]bool{}
	for _, n := range names {
		n = filepath.Clean(n)
		d, f := filepath.Split(n)
		id, ok := strings.CutSuffix(f, ".lock")
		if !ok || !conversationID.MatchString(id) {
			continue
		}
		if real, err := filepath.EvalSymlinks(filepath.Dir(d)); err == nil {
			d = real
		} else {
			d = filepath.Clean(d)
		}
		if samePath(d, presence) || samePath(filepath.Clean(filepath.Dir(n)), filepath.Join(dir, "presence")) {
			ids[strings.ToLower(id)] = true
		}
	}
	switch len(ids) {
	case 0:
		return "", "", &chat.Miss{Reason: chat.NoRecordYet}
	case 1:
		for id := range ids {
			p := filepath.Join(dir, "brain", id, ".system_generated", "logs", "transcript.jsonl")
			if _, err := os.Stat(p); err != nil {
				return "", "", &chat.Miss{Reason: chat.NoRecordYet}
			}
			return p, "", nil
		}
	}
	return "", "", &chat.Miss{Reason: chat.Ambiguous}
}

func samePath(a, b string) bool {
	if runtime.GOOS == "windows" || runtime.GOOS == "darwin" {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// descendants: the processes started by pid, and theirs.
func descendants(pid int) []int {
	if runtime.GOOS == "windows" {
		return nil
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "ps", "-A", "-o", "pid=,ppid=").Output()
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
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	out, _ := exec.CommandContext(ctx, lsof, "-nP", "-Fn", "-p", strings.Join(ps, ",")).Output()
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var names []string
	for _, l := range strings.Split(string(out), "\n") {
		if strings.HasPrefix(l, "n/") {
			names = append(names, l[1:])
		}
	}
	return names, nil
}

// --- the record ------------------------------------------------------------------------------------------------

type step struct {
	Index     *int     `json:"step_index"`
	Source    string   `json:"source"`
	Type      string   `json:"type"`
	Status    string   `json:"status"`
	CreatedAt string   `json:"created_at"`
	Content   string   `json:"content"`
	Thinking  string   `json:"thinking"`
	Calls     []call   `json:"tool_calls"`
	Media     []media  `json:"media"`
	Truncated []string `json:"truncated_fields"`
	Error     string   `json:"error"`
}

type call struct {
	Name       string                     `json:"name"`
	Args       map[string]json.RawMessage `json:"args"`
	nativeArgs bool
}

type media struct {
	MimeType string `json:"mime_type"`
	URI      string `json:"uri"`
}

// The short transcript encodes each argument as JSON inside a string; the full transcript uses native JSON values.
func (c call) arg(k string) json.RawMessage {
	v := c.Args[k]
	if c.nativeArgs || v == nil {
		return v
	}
	var s string
	if json.Unmarshal(v, &s) == nil && json.Valid([]byte(s)) {
		return json.RawMessage(s)
	}
	return v
}

func (c call) str(k string) string {
	var s string
	json.Unmarshal(c.arg(k), &s)
	return s
}

// input is the call's arguments as one JSON object, their values decoded.
func (c call) input() json.RawMessage {
	m := map[string]json.RawMessage{}
	for k := range c.Args {
		m[k] = c.arg(k)
	}
	b, _ := json.Marshal(m)
	return b
}

func parse(raw []byte) (step, error) {
	var s step
	if err := json.Unmarshal(raw, &s); err != nil {
		return s, chat.Formatf("a step that is not JSON")
	}
	if s.Index == nil {
		return s, chat.Formatf("a step without step_index")
	}
	return s, nil
}

func full(path string) string { return filepath.Join(filepath.Dir(path), "transcript_full.jsonl") }

// whole reads the steps of full that the window's cut ones need, by number: only lines beginning with one of them are
// decoded. The full file is searched from MaxBack before its end at most (its steps are in the same order).
func whole(path string, need map[int]bool) map[int]step {
	got := map[int]step{}
	if len(need) == 0 {
		return got
	}
	f := full(path)
	st, err := os.Stat(f)
	if err != nil {
		return got
	}
	off := max(st.Size()-chat.MaxBack, 0)
	var rerr error
	for raw := range chat.Lines(f, off, &rerr) {
		n, ok := index(raw)
		if !ok || !need[n] {
			continue
		}
		addFull(got, raw)
		if len(got) == len(need) {
			break
		}
	}
	for n := range need {
		if _, ok := got[n]; ok || off == 0 {
			continue
		}
		if at, size, err := findStep(f, n); err == nil {
			for raw := range chat.LinesUntil(f, at, min(size, at+chat.MaxBack), &rerr) {
				addFull(got, raw)
				break
			}
		}
	}
	return got
}

func addFull(got map[int]step, raw []byte) {
	if s, err := parse(raw); err == nil {
		for i := range s.Calls {
			s.Calls[i].nativeArgs = true
		}
		got[*s.Index] = s
	}
}

var indexHead = regexp.MustCompile(`^\{"step_index":(\d+)[,}]`)

// index reads a line's step number: quickly from its start, where agy writes it, else from the whole line.
func index(raw []byte) (int, bool) {
	if m := indexHead.FindSubmatch(raw); m != nil {
		n, err := strconv.Atoi(string(m[1]))
		return n, err == nil
	}
	if !bytes.Contains(raw, []byte(`"step_index"`)) {
		return 0, false
	}
	s, err := parse(raw)
	if err != nil {
		return 0, false
	}
	return *s.Index, true
}

// findStep searches the append-only record's ordered step numbers without reading the intervening turns.
func findStep(path string, target int) (int64, int64, error) {
	f, err := os.Open(path)
	if err != nil {
		return 0, 0, err
	}
	defer f.Close()
	st, err := f.Stat()
	if err != nil {
		return 0, 0, err
	}
	size := st.Size()
	for lo, hi := int64(0), size; lo < hi; {
		mid := lo + (hi-lo)/2
		at := mid
		if at > lo {
			at-- // Include the newline if mid is already a record boundary.
		}
		r := bufio.NewReader(io.NewSectionReader(f, at, min(size-at, chat.MaxBack)))
		if mid > lo {
			tail, err := r.ReadBytes('\n')
			if err != nil {
				hi = mid
				continue
			}
			at += int64(len(tail))
		}
		var raw []byte
		var readErr error
		for at < hi {
			raw, readErr = r.ReadBytes('\n')
			if readErr != nil || len(bytes.TrimSpace(raw)) > 0 {
				break
			}
			at += int64(len(raw))
		}
		if at >= hi || readErr == io.EOF {
			hi = mid
			continue
		}
		if readErr != nil {
			return 0, 0, readErr
		}
		n, ok := index(bytes.TrimSpace(raw))
		if !ok {
			return 0, 0, chat.Formatf("a step without step_index")
		}
		switch {
		case n < target:
			lo = at + int64(len(raw))
		case n > target:
			hi = mid
		default:
			return at, size, nil
		}
	}
	return 0, 0, chat.ErrNoStep
}

// isPrompt: a line that begins a turn (a prompt of the person's, not agy's plan-mode handoff).
func isPrompt(raw []byte) bool {
	if !bytes.Contains(raw, []byte(`"type":"USER_INPUT"`)) {
		return false
	}
	s, err := parse(raw)
	return err == nil && s.Type == "USER_INPUT" && !handoff(said(s.Content)) && !reviewAnswer.MatchString(said(s.Content))
}

func window(path string) (int64, bool, error) {
	return chat.Start(path, []byte(`"type":"USER_INPUT"`), isPrompt)
}

var (
	request = regexp.MustCompile(`(?s)<USER_REQUEST>\n?(.*?)\n?</USER_REQUEST>`)
	// agy's own blocks around what was typed.
	wrapper    = regexp.MustCompile(`(?s)<(ADDITIONAL_METADATA|USER_SETTINGS_CHANGE|SYSTEM_MESSAGE)>.*?</(ADDITIONAL_METADATA|USER_SETTINGS_CHANGE|SYSTEM_MESSAGE)>`)
	systemNote = regexp.MustCompile(`^The following is a <SYSTEM_MESSAGE> not actually sent by the user\.[^\n]*\n*`)
	// The note agy sends at the start of a plan-mode turn run by another model: its, not the person's.
	planReview = regexp.MustCompile(`^\[plan review\]`)
	// What agy sends for the person when they approve or reject a plan in /artifact.
	reviewAnswer = regexp.MustCompile(`^\[(Approved|Rejected)\] `)
)

// said is what the person typed: the words inside <USER_REQUEST>, or the prompt without agy's blocks.
func said(content string) string {
	if m := request.FindStringSubmatch(content); m != nil {
		return strings.TrimSpace(m[1])
	}
	return strings.TrimSpace(wrapper.ReplaceAllString(systemNote.ReplaceAllString(content, ""), ""))
}

func handoff(s string) bool { return planReview.MatchString(s) }

// clean is a result's words without agy's time lines over them.
var timeLine = regexp.MustCompile(`^(Created|Completed) At: [^\n]*\n?`)

func clean(s string) string {
	for {
		loc := timeLine.FindStringIndex(s)
		if loc == nil || loc[0] != 0 {
			return s
		}
		s = s[loc[1]:]
	}
}

type pending struct {
	id string
	c  call
}

type building struct {
	t      chat.Turn
	start  time.Time
	last   time.Time
	texts  []string // the model's words since its last call: the reply, if nothing follows
	textID string
	calls  []pending // calls waiting for their results, in order
	asked  map[string]*chat.Ask
	// spoken: agy's own message came after the model's words, so words after it start the reply again.
	spoken bool
	// review: the turn wrote a plan for the person to review; agy waits on it (blocked, "Approve plan") until a prompt
	// says [Approved] or [Rejected].
	review bool
	// head: the steps before the window's first prompt, a turn that began before the window. Shown only when the
	// window has no prompt (its prompt is further back than MaxBack), so a long turn is never no turn at all.
	head bool
}

func (b *building) flushNotes() {
	if len(b.texts) == 0 {
		return
	}
	first := strings.TrimSpace(strings.SplitN(strings.TrimSpace(strings.Join(b.texts, "\n\n")), "\n", 2)[0])
	b.t.Steps = append(b.t.Steps, chat.Step{ID: b.textID, Kind: "note", Title: title(first)})
	b.texts, b.textID = nil, ""
}

func (b *building) finish() chat.Turn {
	t := b.t
	t.Reply = strings.Join(b.texts, "\n\n")
	if t.End == "open" {
		for _, p := range b.calls {
			if a := b.asked[p.id]; a != nil {
				t.Ask = a
			}
		}
	}
	if t.End != "open" && !b.start.IsZero() && b.last.After(b.start) {
		t.Secs = int(b.last.Sub(b.start).Seconds())
	}
	if t.Steps == nil {
		t.Steps = []chat.Step{}
	}
	return t
}

func when(s string) time.Time { t, _ := time.Parse(time.RFC3339Nano, s); return t }

// Turns reads the record's end, again only when it (or its full copy) changed: the phone asks every 3 s while the
// agent works.
func (r *Reader) Turns(path string) ([]chat.Turn, error) {
	st, err := os.Stat(path)
	if err != nil {
		return nil, err
	}
	var fullSize int64 = -1
	if fs, err := os.Stat(full(path)); err == nil {
		fullSize = fs.Size()
	}
	ino := chat.Inode(st)
	r.mu.Lock()
	defer r.mu.Unlock()
	if c, ok := r.cache[path]; ok && c.size == st.Size() && c.fullSize == fullSize && c.mod.Equal(st.ModTime()) && c.ino == ino {
		return c.turns, nil
	}
	r.parses++
	off, from0, err := window(path)
	if err != nil {
		return nil, err
	}
	ts, err := r.parseWindow(path, off, -1, from0)
	if err != nil {
		return nil, err
	}
	if len(ts) > chat.MaxTurns {
		ts = ts[len(ts)-chat.MaxTurns:]
	}
	if r.cache == nil {
		r.cache = map[string]parsed{}
	}
	r.cache[path] = parsed{size: st.Size(), fullSize: fullSize, mod: st.ModTime(), ino: ino, turns: ts}
	return ts, nil
}

func (r *Reader) Before(path, first string) (string, error) { return chat.FileBefore(path, first) }

func (r *Reader) History(path, before string) (chat.HistoryPage, error) {
	return chat.FileHistory(path, before, isPrompt, func(off, end int64) ([]chat.Turn, error) {
		return r.parseWindow(path, off, end, off == 0)
	})
}

// steps reads the record's steps from off to end, the cut ones whole from the full copy.
func steps(path string, off, end int64, stop func(step) bool) ([]step, error) {
	var out []step
	need := map[int]bool{}
	var rerr error
	for raw := range chat.LinesUntil(path, off, end, &rerr) {
		s, err := parse(raw)
		if err != nil {
			return nil, err
		}
		if len(s.Truncated) > 0 {
			need[*s.Index] = true
		}
		out = append(out, s)
		if stop != nil && stop(s) {
			break
		}
	}
	if rerr != nil {
		return nil, rerr
	}
	got := whole(path, need)
	for i, s := range out {
		if w, ok := got[*s.Index]; ok && w.Type == s.Type {
			w.Truncated = nil
			out[i] = w
		}
	}
	return out, nil
}

func (r *Reader) parseWindow(path string, off, end int64, from0 bool) ([]chat.Turn, error) {
	ss, err := steps(path, off, end, nil)
	if err != nil {
		return nil, err
	}
	var out []chat.Turn
	var cur *building
	if !from0 {
		cur = &building{t: chat.Turn{ID: "earlier", From: "you", End: "open"}, asked: map[string]*chat.Ask{}, head: true}
	}
	for _, s := range ss {
		n := strconv.Itoa(*s.Index)
		switch {
		case s.Type == "USER_INPUT":
			text := said(s.Content)
			if handoff(text) {
				continue
			}
			// The answer to a plan's review goes on the same turn: the person's step, then the work it starts.
			if cur != nil && !cur.head && cur.review && reviewAnswer.MatchString(text) {
				cur.flushNotes()
				cur.t.Steps = append(cur.t.Steps, chat.Step{ID: n, Kind: chat.StepYou, Title: text})
				cur.review, cur.t.End = false, "open"
				continue
			}
			if cur != nil && !cur.head {
				if cur.t.End == "open" {
					// Cut off: the next prompt came before the turn's end.
					cur.t.End = "stopped"
					cur.calls = nil
				}
				out = append(out, cur.finish())
			}
			var images []chat.Image
			for i := range s.Media {
				images = append(images, chat.Image{ID: fmt.Sprintf("%s.m%d", n, i)})
			}
			cur = &building{t: chat.Turn{ID: n, From: "you", Said: text, At: s.CreatedAt, Images: images, End: "open"}, start: when(s.CreatedAt), asked: map[string]*chat.Ask{}}
		case cur == nil:
		case s.Type == "SYSTEM_MESSAGE":
			cur.spoken = len(cur.texts) > 0
		case s.Type == "PLANNER_RESPONSE":
			if cur.spoken && (strings.TrimSpace(s.Content) != "" || len(s.Calls) > 0) {
				cur.flushNotes()
			}
			cur.spoken = false
			if t := strings.TrimSpace(s.Thinking); t != "" {
				cur.flushNotes()
				cur.t.Steps = append(cur.t.Steps, chat.Step{ID: n + ".t", Kind: "think", Title: title(t)})
			}
			if strings.TrimSpace(s.Content) != "" {
				if cur.textID == "" {
					cur.textID = n + ".n"
				}
				cur.texts = append(cur.texts, s.Content)
			}
			if len(s.Calls) > 0 {
				cur.flushNotes()
			}
			for i, c := range s.Calls {
				id := fmt.Sprintf("%s.%d", n, i)
				cur.calls = append(cur.calls, pending{id: id, c: c})
				if c.Name == "ask_question" {
					cur.asked[id] = chat.AskOf(c.arg("questions"))
					// An ask's questions, under the key AskOf reads.
					if len(cur.asked[id].Questions) == 0 {
						cur.asked[id] = chat.AskOf(c.input())
					}
					continue
				}
				kind, name := describe(c)
				cur.t.Steps = append(cur.t.Steps, chat.Step{ID: id, Kind: kind, Title: title(name)})
			}
			cur.last = when(s.CreatedAt)
			if len(s.Calls) > 0 {
				cur.t.End = "open"
			} else if s.Status == "DONE" && len(cur.calls) == 0 {
				cur.t.End = "done"
			}
		case s.Source == "MODEL" && len(cur.calls) > 0:
			// The result of the first call waiting for one.
			p := cur.calls[0]
			cur.calls = cur.calls[1:]
			failed := s.Status == "ERROR" || s.Status == "INVALID" || s.Status == "CANCELED" || s.Status == "CANCELLED"
			out := clean(s.Content)
			if p.c.Name == "run_command" {
				if m := exitLine.FindStringSubmatch(out); m != nil && m[1] != "0" {
					failed = true
				}
			}
			if a, ok := cur.asked[p.id]; ok {
				delete(cur.asked, p.id)
				cur.t.Steps = append(cur.t.Steps, chat.Step{ID: p.id, Kind: "other", Title: title(a.Text), Failed: failed})
			}
			for i := range cur.t.Steps {
				st := &cur.t.Steps[i]
				if st.ID != p.id {
					continue
				}
				st.Failed = failed
				if st.Kind == "edit" && !failed {
					if c := change(p.c, out); c != nil {
						st.Added, st.Deleted = c.Added, c.Deleted
					}
				}
			}
			cur.last = when(s.CreatedAt)
			cur.t.End = "open"
			_, plan := isPlan(p.c)
			cur.review = plan && p.c.Name != "exit_plan_mode" && !failed
			// Declined at its approval: agy writes the call as failed and waits at its prompt.
			if s.Status == "ERROR" && strings.Contains(s.Error, "user denied permission") {
				cur.t.End = "stopped"
			}
		}
	}
	if cur != nil && cur.review && cur.t.End == "done" {
		cur.t.End = "open"
	}
	if cur != nil && !(cur.head && len(cur.t.Steps) == 0 && len(cur.texts) == 0) {
		out = append(out, cur.finish())
	}
	return out, nil
}

// title is a step's line: the first line, at most 80 bytes. Details come only through Detail.
func title(s string) string {
	return chat.Cut(strings.TrimSpace(strings.SplitN(strings.TrimSpace(s), "\n", 2)[0]), 80)
}

// home writes a path under the home folder as ~/…, shorter on a phone.
func home(p string) string {
	h, err := os.UserHomeDir()
	if err != nil || h == "" {
		return p
	}
	if rest, ok := strings.CutPrefix(p, h+string(filepath.Separator)); ok {
		return filepath.Join("~", rest)
	}
	return p
}

// isPlan: a call that writes the plan agy asks the person to review (plan mode's implementation plan), or the plan
// of an exit_plan_mode call.
func isPlan(c call) (string, bool) {
	if c.Name == "exit_plan_mode" {
		return c.str("plan"), true
	}
	if c.Name != "write_file" && c.Name != "edit_file" {
		return "", false
	}
	var meta struct {
		RequestFeedback bool `json:"RequestFeedback"`
	}
	if json.Unmarshal(c.arg("ArtifactMetadata"), &meta) != nil || !meta.RequestFeedback {
		return "", false
	}
	if c.Name == "write_file" {
		return c.str("Content"), true
	}
	return c.str("Replacement"), true
}

// planTitle: the plan's first line, without its heading marks.
func planTitle(plan, file string) string {
	for _, l := range strings.Split(plan, "\n") {
		if l = strings.TrimSpace(strings.TrimLeft(strings.TrimSpace(l), "#")); l != "" {
			return l
		}
	}
	return filepath.Base(file)
}

// describe gives a call its kind and a short title. A tool it doesn't know is "other", titled with its name.
func describe(c call) (string, string) {
	or := func(xs ...string) string {
		for _, x := range xs {
			if strings.TrimSpace(x) != "" {
				return strings.TrimSpace(x)
			}
		}
		return c.Name
	}
	if plan, ok := isPlan(c); ok {
		return "plan", planTitle(plan, c.str("Path"))
	}
	switch c.Name {
	case "read_file", "view_file":
		return "read", or(home(c.str("Path")), home(c.str("AbsolutePath")))
	case "list_dir":
		return "read", or(home(c.str("DirectoryPath")), home(c.str("Path")))
	case "edit_file", "write_file", "replace_file_content", "multi_replace_file_content", "write_to_file":
		return "edit", or(home(c.str("Path")), home(c.str("TargetFile")))
	case "run_command":
		return "run", or(strings.SplitN(c.str("CommandLine"), "\n", 2)[0], c.str("toolSummary"))
	case "grep_search", "find_by_name", "codebase_search":
		return "search", or(c.str("Query"), c.str("Pattern"), c.str("query"))
	case "search_web":
		return "web", or(c.str("query"), c.str("Query"))
	case "read_url_content", "browser_subagent":
		return "web", or(c.str("Url"), c.str("url"), c.str("Task"))
	case "invoke_subagent":
		return "agent", or(c.str("Role"), c.str("TypeName"))
	case "define_subagent":
		return "agent", or(c.str("Name"))
	case "swarm":
		return "agent", or(c.str("title"), c.str("name"), c.str("brief"), c.str("action"))
	case "run_workflow":
		return "agent", or(c.str("Description"), c.str("ScriptPath"))
	case "send_message":
		return "agent", or(strings.SplitN(c.str("Message"), "\n", 2)[0], c.str("Recipient"))
	case "manage_subagents":
		return "agent", or(c.str("Action"))
	}
	return "other", c.Name
}

// The lines a run_command's result begins with, as agy 1.5.14 writes them.
var (
	exitLine   = regexp.MustCompile(`(?m)^Command exited with code (-?\d+)\.`)
	outputHead = regexp.MustCompile(`(?s)Command output:\n(.*)$`)
	// An edit's result: where the first replacement starts, and the lines it added and removed.
	editLine = regexp.MustCompile(`starting at line (\d+) \(\+(\d+)/-(\d+) lines\)`)
	readLine = regexp.MustCompile(`^Showing lines (\d+)-(\d+) of (\d+)`)
)

// Detail finds a step again: a call's input and result, a note's or a thought's whole text.
func (r *Reader) Detail(path, id string) (chat.Detail, error) {
	n, rest, ok := strings.Cut(id, ".")
	idx, err := strconv.Atoi(n)
	if !ok || err != nil || idx < 0 {
		return chat.Detail{}, chat.ErrNoStep
	}
	off, size, err := findStep(path, idx)
	if err != nil {
		return chat.Detail{}, err
	}
	ss, err := steps(path, off, min(size, off+chat.MaxBack), func(s step) bool {
		if *s.Index == idx {
			return rest == "t"
		}
		if (s.Type == "USER_INPUT" && !handoff(said(s.Content))) || (rest != "n" && s.Type == "PLANNER_RESPONSE") {
			return true
		}
		return rest == "n" && (s.Type != "PLANNER_RESPONSE" || len(s.Calls) > 0)
	})
	if err != nil {
		return chat.Detail{}, err
	}
	for i, s := range ss {
		if *s.Index != idx || s.Type != "PLANNER_RESPONSE" {
			continue
		}
		switch rest {
		case "n":
			// The note: its words, and the words of the responses after it up to the next call.
			text := s.Content
			for _, x := range ss[i+1:] {
				if x.Type != "PLANNER_RESPONSE" {
					break
				}
				if strings.TrimSpace(x.Content) != "" {
					text += "\n\n" + x.Content
				}
				if len(x.Calls) > 0 {
					break
				}
			}
			if strings.TrimSpace(s.Content) == "" {
				return chat.Detail{}, chat.ErrNoStep
			}
			return chat.Detail{Input: chat.Cut(text, chat.MaxDetail)}, nil
		case "t":
			if strings.TrimSpace(s.Thinking) == "" {
				return chat.Detail{}, chat.ErrNoStep
			}
			return chat.Detail{Input: chat.Cut(s.Thinking, chat.MaxDetail)}, nil
		}
		k, err := strconv.Atoi(rest)
		if err != nil || k < 0 || k >= len(s.Calls) || strconv.Itoa(k) != rest {
			return chat.Detail{}, chat.ErrNoStep
		}
		c := s.Calls[k]
		// Its result: the k-th step from the source MODEL after it that isn't a response.
		var res *step
		seen := 0
		for j := i + 1; j < len(ss); j++ {
			x := ss[j]
			if x.Type == "USER_INPUT" && !handoff(said(x.Content)) {
				break
			}
			if x.Type == "PLANNER_RESPONSE" {
				break
			}
			if x.Source != "MODEL" {
				continue
			}
			if seen == k {
				res = &ss[j]
				break
			}
			seen++
		}
		return shaped(c, res), nil
	}
	return chat.Detail{}, chat.ErrNoStep
}

// shaped is a call and its result as the phone shows them: a command, a change to a file, the lines read, a plan.
func shaped(c call, res *step) chat.Detail {
	var d chat.Detail
	var pretty bytes.Buffer
	if json.Indent(&pretty, c.input(), "", "  ") != nil {
		pretty.Write(c.input())
	}
	d.Input = chat.Cut(pretty.String(), chat.MaxDetail)
	out, failed := "", false
	if res != nil {
		out = clean(res.Content)
		failed = res.Status == "ERROR" || res.Status == "INVALID"
		d.Output = chat.Cut(out, chat.MaxDetail)
	}
	if plan, ok := isPlan(c); ok {
		d.Input = chat.Cut(plan, chat.MaxDetail)
		return d
	}
	switch c.Name {
	case "run_command":
		run := &chat.Run{Command: c.str("CommandLine"), Failed: failed}
		text := out
		if m := exitLine.FindStringSubmatch(out); m != nil {
			run.Code, _ = strconv.Atoi(m[1])
			run.Failed = run.Failed || run.Code != 0
			text = ""
			if o := outputHead.FindStringSubmatch(out); o != nil {
				text = o[1]
			}
		}
		text = strings.TrimRight(strings.ReplaceAll(text, "\r\n", "\n"), "\n")
		if text != "" {
			run.Lines = strings.Count(text, "\n") + 1
		}
		run.Output = chat.CutEnd(text, chat.MaxDetail)
		d.Run = run
	case "edit_file", "write_file":
		if res != nil && !failed {
			d.Change = change(c, out)
		}
	case "read_file":
		if m := readLine.FindStringSubmatch(out); m != nil && !failed {
			from, _ := strconv.Atoi(m[1])
			to, _ := strconv.Atoi(m[2])
			total, _ := strconv.Atoi(m[3])
			d.Read = &chat.Read{File: c.str("Path"), From: from, Count: to - from + 1, Total: total}
		}
	}
	return d
}

func lines(s string) []string {
	if s == "" {
		return nil
	}
	return strings.Split(strings.TrimSuffix(s, "\n"), "\n")
}

// change is what an edit_file or write_file did to its file: write_file's lines all added; edit_file's Target and
// Replacement as a diff (the lines they share at either end kept as context), where agy says the change starts.
func change(c call, result string) *chat.Change {
	ch := &chat.Change{File: c.str("Path"), Hunks: []chat.Hunk{}}
	var h chat.Hunk
	if c.Name == "write_file" {
		h = chat.Hunk{OldStart: 0, NewStart: 1}
		for _, l := range lines(c.str("Content")) {
			h.Lines = append(h.Lines, "+"+l)
		}
		ch.Added = len(h.Lines)
	} else {
		old, nw := lines(c.str("Target")), lines(c.str("Replacement"))
		pre := 0
		for pre < len(old) && pre < len(nw) && old[pre] == nw[pre] {
			pre++
		}
		suf := 0
		for suf < len(old)-pre && suf < len(nw)-pre && old[len(old)-1-suf] == nw[len(nw)-1-suf] {
			suf++
		}
		start := 1
		if m := editLine.FindStringSubmatch(result); m != nil {
			start, _ = strconv.Atoi(m[1])
		}
		h = chat.Hunk{OldStart: start, NewStart: start}
		for _, l := range old[:pre] {
			h.Lines = append(h.Lines, " "+l)
		}
		for _, l := range old[pre : len(old)-suf] {
			h.Lines = append(h.Lines, "-"+l)
			ch.Deleted++
		}
		for _, l := range nw[pre : len(nw)-suf] {
			h.Lines = append(h.Lines, "+"+l)
			ch.Added++
		}
		for _, l := range old[len(old)-suf:] {
			h.Lines = append(h.Lines, " "+l)
		}
		// agy's own count covers every place it replaced the target.
		if m := editLine.FindStringSubmatch(result); m != nil {
			ch.Added, _ = strconv.Atoi(m[2])
			ch.Deleted, _ = strconv.Atoi(m[3])
		}
	}
	if len(h.Lines) > chat.MaxChangeLines {
		h.Lines, ch.Cut = h.Lines[:chat.MaxChangeLines], true
	}
	if len(h.Lines) > 0 {
		ch.Hunks = append(ch.Hunks, h)
	}
	return ch
}

// Image finds an image of a prompt again ("<step>.m<n>"): only a file its record names, inside the conversation's own
// folder (brain/<id>/, where agy keeps what was pasted), of an image type, read where it really is.
func (r *Reader) Image(path, id string) (string, []byte, error) {
	n, rest, ok := strings.Cut(id, ".m")
	idx, err1 := strconv.Atoi(n)
	k, err2 := strconv.Atoi(rest)
	if !ok || err1 != nil || err2 != nil || idx < 0 || k < 0 || strconv.Itoa(k) != rest {
		return "", nil, chat.ErrNoStep
	}
	off, size, err := findStep(path, idx)
	if err != nil {
		return "", nil, err
	}
	ss, err := steps(path, off, min(size, off+chat.MaxBack), func(step) bool { return true })
	if err != nil {
		return "", nil, err
	}
	for _, s := range ss {
		if *s.Index != idx || s.Type != "USER_INPUT" || k >= len(s.Media) {
			continue
		}
		m := s.Media[k]
		if !chat.ImageTypes[m.MimeType] {
			return "", nil, chat.ErrNoStep
		}
		file, ok := inside(conversationDir(path), m.URI)
		if !ok {
			return "", nil, chat.ErrNoStep
		}
		f, err := os.Open(file)
		if err != nil {
			return "", nil, chat.ErrNoStep
		}
		defer f.Close()
		const maxImage = 12 << 20
		st, err := f.Stat()
		if err != nil || !st.Mode().IsRegular() || st.Size() > maxImage {
			return "", nil, chat.ErrNoStep
		}
		b, err := io.ReadAll(io.LimitReader(f, maxImage+1))
		if err != nil || len(b) > maxImage {
			return "", nil, chat.ErrNoStep
		}
		// What the file really is, not only what the record says it is.
		kind := http.DetectContentType(b)
		if !chat.ImageTypes[kind] {
			return "", nil, chat.ErrNoStep
		}
		return kind, b, nil
	}
	return "", nil, chat.ErrNoStep
}

// conversationDir: brain/<id>/ of a record at brain/<id>/.system_generated/logs/transcript.jsonl.
func conversationDir(path string) string {
	return filepath.Dir(filepath.Dir(filepath.Dir(path)))
}

// inside: the file a media URI names (a path, or a file: URL), when it is really inside dir, through any links.
func inside(dir, uri string) (string, bool) {
	p := uri
	if u, err := url.Parse(uri); err == nil && u.Scheme == "file" {
		p = u.Path
		if runtime.GOOS == "windows" {
			p = strings.TrimPrefix(p, "/")
		}
	}
	if !filepath.IsAbs(p) {
		return "", false
	}
	real, err := filepath.EvalSymlinks(p)
	if err != nil {
		return "", false
	}
	root, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return "", false
	}
	rel, err := filepath.Rel(root, real)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return "", false
	}
	if st, err := os.Stat(real); err != nil || !st.Mode().IsRegular() {
		return "", false
	}
	return real, true
}

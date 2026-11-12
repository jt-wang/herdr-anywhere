// Package pi is Pi's reader, and the small file that lets it find Pi's conversation. Pi writes each conversation to
// a .jsonl file under its folder's sessions/ (measured with Pi 1.4.0): a `session` line, then entries that
// each name their parent, so a conversation can branch; the one Pi shows is the chain from the last entry back.
// Messages are user, assistant (text, thinking, toolCall) and toolResult.
//
// Which file a pane's Pi writes can't be told from outside: Pi keeps no file open and reports no conversation to
// herdr-core. The file Setup puts in Pi's extensions folder tells it (a chat.Setup).
package pi

import (
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"herdr-anywhere/companion/internal/chat"
)

type Reader struct {
	Setup *Setup

	mu    sync.Mutex
	cache map[string]parsed
}

type parsed struct {
	size  int64
	mod   time.Time
	ino   uint64
	turns []chat.Turn
}

var _ chat.PaneReader = (*Reader)(nil)

// Find isn't used: a Pi's conversation is found from its pane (FindPane).
func (r *Reader) Find(pid int) (string, string, error) {
	return "", "", &chat.Miss{Reason: chat.NoMatch}
}

func (r *Reader) sessionDirs() []string {
	ds := []string{filepath.Join(r.Setup.Dir, "sessions")}
	if d := os.Getenv("PI_CODING_AGENT_SESSION_DIR"); d != "" {
		ds = append(ds, d)
	}
	return ds
}

func (r *Reader) FindPane(p chat.Pane) (string, string, error) {
	f, ok := p.Labeled()
	if !ok {
		if r.Setup == nil {
			return "", "", &chat.Miss{Reason: chat.NoMatch}
		}
		if st := r.Setup.State(); st == chat.Absent || st == chat.NoAgent {
			return "", "", &chat.Miss{Reason: chat.SetupNeeded}
		}
		return "", "", &chat.Miss{Reason: chat.SetupNotLoaded}
	}
	f = filepath.Clean(f)
	inside := false
	for _, d := range r.sessionDirs() {
		if d != "" && strings.HasPrefix(f, filepath.Clean(d)+string(filepath.Separator)) {
			inside = true
		}
	}
	if !inside || filepath.Ext(f) != ".jsonl" {
		return "", "", &chat.Miss{Reason: chat.NoMatch}
	}
	if _, err := os.Stat(f); err != nil {
		return "", "", &chat.Miss{Reason: chat.NoRecordYet}
	}
	return f, "", nil
}

// entry is one line of a conversation file.
type entry struct {
	Type      string          `json:"type"`
	ID        string          `json:"id"`
	ParentID  *string         `json:"parentId"`
	Timestamp string          `json:"timestamp"`
	Cwd       string          `json:"cwd"`
	Message   json.RawMessage `json:"message"`
}

type part struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	Thinking  string          `json:"thinking"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Arguments json.RawMessage `json:"arguments"`
	Data      string          `json:"data"`
	MimeType  string          `json:"mimeType"`
}

type message struct {
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	StopReason string          `json:"stopReason"`
	ToolCallID string          `json:"toolCallId"`
	ToolName   string          `json:"toolName"`
	IsError    bool            `json:"isError"`
	Details    json.RawMessage `json:"details"`
	Timestamp  int64           `json:"timestamp"`
}

func (m *message) parts() []part {
	var ps []part
	if json.Unmarshal(m.Content, &ps) == nil {
		return ps
	}
	var s string
	if json.Unmarshal(m.Content, &s) == nil && s != "" {
		return []part{{Type: "text", Text: s}}
	}
	return nil
}

func (m *message) text() string {
	var ts []string
	for _, p := range m.parts() {
		if p.Type == "text" && strings.TrimSpace(p.Text) != "" {
			ts = append(ts, p.Text)
		}
	}
	return strings.Join(ts, "\n\n")
}

type args struct {
	Command string `json:"command"`
	Path    string `json:"path"`
	Content string `json:"content"`
	Pattern string `json:"pattern"`
}

// msg is a message of the conversation, with the entry it is in.
type msg struct {
	id, at string
	m      message
}

// window is where a conversation file is read from: its last MaxTail bytes, or from the person's last message when that
// is further back (a turn running commands with large outputs).
func window(path string) (int64, error) {
	off, _, err := chat.Start(path, []byte(`"role":"user"`), isUser)
	return off, err
}

func isUser(raw []byte) bool {
	var e entry
	var m message
	return json.Unmarshal(raw, &e) == nil && e.Type == "message" && json.Unmarshal(e.Message, &m) == nil && m.Role == "user"
}

// branch reads a conversation file's end: its messages along the chain from the last entry back, and its folder. cut:
// the chain goes on before the window.
func branch(path string) (_ []msg, cwd string, cut bool, _ error) {
	off, err := window(path)
	if err != nil {
		return nil, "", false, err
	}
	byID := map[string]entry{}
	var last string
	var rerr error
	for raw := range chat.Lines(path, off, &rerr) {
		var e entry
		if json.Unmarshal(raw, &e) != nil {
			return nil, "", false, chat.Formatf("a line that isn't JSON")
		}
		if e.Type == "session" {
			cwd = e.Cwd
			continue
		}
		if e.ID == "" {
			continue
		}
		byID[e.ID] = e
		last = e.ID
	}
	if rerr != nil {
		return nil, "", false, rerr
	}
	var chain []entry
	for id, n := last, 0; id != "" && n <= len(byID); n++ {
		e, ok := byID[id]
		if !ok {
			cut = true // the window starts here
			break
		}
		chain = append(chain, e)
		if e.ParentID == nil {
			break
		}
		id = *e.ParentID
	}
	var out []msg
	for i := len(chain) - 1; i >= 0; i-- {
		e := chain[i]
		if e.Type != "message" {
			continue
		}
		var m message
		if json.Unmarshal(e.Message, &m) != nil {
			return nil, "", false, chat.Formatf("message %s", e.ID)
		}
		out = append(out, msg{id: e.ID, at: e.Timestamp, m: m})
	}
	return out, cwd, cut, nil
}

func rel(p, cwd string) string {
	if cwd != "" && filepath.IsAbs(p) {
		if r, err := filepath.Rel(cwd, p); err == nil && !strings.HasPrefix(r, "..") {
			return r
		}
	}
	return p
}

func title(s string) string {
	return chat.Cut(strings.TrimSpace(strings.SplitN(strings.TrimSpace(s), "\n", 2)[0]), 80)
}

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(s, "\n"), "\n") + 1
}

// patchCounts: the lines a unified diff adds and removes.
func patchCounts(diff string) (added, deleted int) {
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

func patchOf(details json.RawMessage) string {
	var d struct {
		Patch string `json:"patch"`
	}
	json.Unmarshal(details, &d)
	return d.Patch
}

// toolStep: the step a tool call shows as. result is its toolResult, nil while it runs.
func toolStep(p part, result *message, cwd string) chat.Step {
	var a args
	json.Unmarshal(p.Arguments, &a)
	failed := result != nil && result.IsError
	s := chat.Step{ID: p.ID, Failed: failed}
	switch p.Name {
	case "bash":
		s.Kind, s.Title = "run", title(a.Command)
	case "edit":
		s.Kind, s.Title = "edit", rel(a.Path, cwd)
		if result != nil {
			s.Added, s.Deleted = patchCounts(patchOf(result.Details))
		}
	case "write":
		s.Kind, s.Title, s.Added = "edit", rel(a.Path, cwd), lineCount(a.Content)
	case "read":
		s.Kind, s.Title = "read", rel(a.Path, cwd)
	case "grep", "find":
		s.Kind, s.Title = "search", title(a.Pattern)
	case "ls":
		s.Kind, s.Title = "search", rel(a.Path, cwd)
	default:
		s.Kind, s.Title = "other", p.Name
	}
	return s
}

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
	ts, err := parse(path)
	if err != nil {
		return nil, err
	}
	if r.cache == nil {
		r.cache = map[string]parsed{}
	}
	r.cache[path] = parsed{size: st.Size(), mod: st.ModTime(), ino: ino, turns: ts}
	return ts, nil
}

// parse: a person's message opens a turn; the model's texts are notes between its steps, the last one its reply; the
// turn is over when the model stops without calling a tool (stopReason stop), or stopped (aborted, error).
func parse(path string) ([]chat.Turn, error) {
	ms, cwd, cut, err := branch(path)
	if err != nil {
		return nil, err
	}
	results := map[string]*message{}
	for i := range ms {
		if ms[i].m.Role == "toolResult" {
			results[ms[i].m.ToolCallID] = &ms[i].m
		}
	}
	var turns []chat.Turn
	var cur *chat.Turn
	var started, ended int64
	var pending []struct{ id, text string }
	// head: the turn the window starts in, the person's message further back than MaxBack. Shown only when no message
	// of the person's is in the window, so a long turn is never no turn at all.
	head := cut
	if head {
		cur = &chat.Turn{ID: "earlier", From: "you", Steps: []chat.Step{}, End: "open"}
	}
	flush := func() {
		for _, p := range pending {
			cur.Steps = append(cur.Steps, chat.Step{ID: p.id, Kind: "note", Title: title(p.text)})
		}
		pending = nil
	}
	finish := func() {
		if cur == nil {
			return
		}
		var said []string
		for _, p := range pending {
			said = append(said, p.text)
		}
		pending = nil
		if cur.End != "open" {
			cur.Reply = strings.Join(said, "\n\n")
			if ended > started && started > 0 {
				cur.Secs = int((ended - started) / 1000)
			}
		} else {
			for _, s := range said { // still working: what it said so far are notes
				cur.Steps = append(cur.Steps, chat.Step{Kind: "note", Title: title(s)})
			}
		}
		turns = append(turns, *cur)
		cur = nil
	}
	prev := ""
	for _, x := range ms {
		after := prev
		prev = x.m.Role
		switch x.m.Role {
		case "user":
			// Steered in: sent while the agent worked, and taken right after a tool's result, before the turn went on
			// (the model's last word called a tool, so the turn is open). A step of the turn, where it was taken. A
			// follow-up comes after the model stopped, and opens a turn of its own.
			if cur != nil && cur.End == "open" && after == "toolResult" {
				head = false
				flush()
				cur.Steps = append(cur.Steps, chat.Step{ID: x.id, Kind: chat.StepYou, Title: strings.TrimSpace(x.m.text())})
				continue
			}
			if head {
				cur, pending, head = nil, nil, false
			}
			finish()
			cur = &chat.Turn{ID: x.id, From: "you", Said: x.m.text(), At: x.at, Steps: []chat.Step{}, End: "open"}
			for i, p := range x.m.parts() {
				if p.Type == "image" && chat.ImageTypes[p.MimeType] {
					cur.Images = append(cur.Images, chat.Image{ID: fmt.Sprintf("%s:%d", x.id, i)})
				}
			}
			started, ended = x.m.Timestamp, 0
		case "assistant":
			if cur == nil {
				continue
			}
			for i, p := range x.m.parts() {
				switch p.Type {
				case "text":
					if strings.TrimSpace(p.Text) != "" {
						pending = append(pending, struct{ id, text string }{fmt.Sprintf("%s:%d", x.id, i), p.Text})
					}
				case "thinking":
					if t := title(p.Thinking); t != "" {
						flush()
						cur.Steps = append(cur.Steps, chat.Step{ID: fmt.Sprintf("%s:%d", x.id, i), Kind: "think", Title: t})
					}
				case "toolCall":
					flush()
					cur.Steps = append(cur.Steps, toolStep(p, results[p.ID], cwd))
				}
			}
			ended = x.m.Timestamp
			switch x.m.StopReason {
			case "stop", "length":
				cur.End = "done"
			case "aborted", "error":
				cur.End = "stopped"
			default:
				cur.End = "open"
			}
		}
	}
	finish()
	if len(turns) > chat.MaxTurns {
		turns = turns[len(turns)-chat.MaxTurns:]
	}
	return turns, nil
}

var exited = regexp.MustCompile(`\n*Command exited with code (\d+)\s*$`)

// Detail finds a step again: a command and what it printed, a file's change, a note's or a thought's whole text, a
// read file's text and images.
func (r *Reader) Detail(path, step string) (chat.Detail, error) {
	ms, cwd, _, err := branch(path)
	if err != nil {
		return chat.Detail{}, err
	}
	var call *part
	var result *message
	for i := range ms {
		m := &ms[i].m
		if m.Role == "toolResult" && m.ToolCallID == step {
			result = m
		}
		for j, p := range m.parts() {
			if m.Role == "assistant" && p.Type == "toolCall" && p.ID == step {
				p := p
				call = &p
			}
			if fmt.Sprintf("%s:%d", ms[i].id, j) == step {
				switch p.Type {
				case "text":
					return chat.Detail{Output: chat.Cut(p.Text, chat.MaxDetail)}, nil
				case "thinking":
					return chat.Detail{Output: chat.Cut(p.Thinking, chat.MaxDetail)}, nil
				}
			}
		}
	}
	if call == nil {
		return chat.Detail{}, chat.ErrNoStep
	}
	var a args
	json.Unmarshal(call.Arguments, &a)
	out := ""
	if result != nil {
		out = result.text()
	}
	switch call.Name {
	case "bash":
		run := &chat.Run{Command: a.Command, Failed: result != nil && result.IsError}
		if m := exited.FindStringSubmatch(out); m != nil {
			run.Code, _ = strconv.Atoi(m[1])
			out = out[:len(out)-len(m[0])]
		}
		out = strings.TrimRight(out, "\n")
		run.Output, run.Lines = chat.CutEnd(out, chat.MaxDetail), lineCount(out)
		return chat.Detail{Input: chat.Cut(a.Command, chat.MaxDetail), Output: run.Output, Run: run}, nil
	case "edit":
		c := &chat.Change{File: rel(a.Path, cwd), Hunks: []chat.Hunk{}}
		if result != nil {
			diff := patchOf(result.Details)
			c.Hunks, c.Cut = chat.DiffHunks(diff)
			c.Added, c.Deleted = patchCounts(diff)
		}
		return chat.Detail{Input: c.File, Output: chat.Cut(out, chat.MaxDetail), Change: c}, nil
	case "write":
		c := &chat.Change{File: rel(a.Path, cwd), Added: lineCount(a.Content)}
		h := chat.Hunk{OldStart: 0, NewStart: 1}
		for _, l := range strings.Split(strings.TrimSuffix(a.Content, "\n"), "\n") {
			if len(h.Lines) >= chat.MaxChangeLines {
				c.Cut = true
				break
			}
			h.Lines = append(h.Lines, "+"+l)
		}
		c.Hunks = []chat.Hunk{h}
		return chat.Detail{Input: c.File, Output: chat.Cut(out, chat.MaxDetail), Change: c}, nil
	}
	d := chat.Detail{Input: chat.Cut(string(call.Arguments), chat.MaxDetail), Output: chat.Cut(out, chat.MaxDetail)}
	if call.Name == "read" {
		d.Input = rel(a.Path, cwd)
	}
	if result != nil {
		for i, p := range result.parts() {
			if p.Type == "image" && chat.ImageTypes[p.MimeType] {
				d.Images = append(d.Images, chat.Image{ID: fmt.Sprintf("%s#%d", step, i), Source: a.Path})
			}
		}
	}
	return d, nil
}

// Image finds an image again: one a person sent (entry id:n) or one in a tool's result (call id#n).
func (r *Reader) Image(path, id string) (string, []byte, error) {
	ms, _, _, err := branch(path)
	if err != nil {
		return "", nil, err
	}
	for _, x := range ms {
		for i, p := range x.m.parts() {
			if p.Type != "image" || !chat.ImageTypes[p.MimeType] {
				continue
			}
			if (x.m.Role == "user" && id == fmt.Sprintf("%s:%d", x.id, i)) ||
				(x.m.Role == "toolResult" && id == fmt.Sprintf("%s#%d", x.m.ToolCallID, i)) {
				b, err := base64.StdEncoding.DecodeString(p.Data)
				if err != nil {
					return "", nil, chat.ErrNoStep
				}
				return p.MimeType, b, nil
			}
		}
	}
	return "", nil, chat.ErrNoStep
}

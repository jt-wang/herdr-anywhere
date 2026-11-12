// Package claude is Claude Code's reader. The pid file sessions/<pid>.json names the session a process is in; the
// transcript projects/<folder>/<session id>.jsonl holds its turns. Rules measured on 2.1.319
// (CHAT_LIVE=1 go test -run Live re-checks them): a prompt is a user line with text and a turnPosition; turnOrigin "human" or "sdk" is the
// person; each assistant line holds one content block, and a turn ends with stop_reason end_turn.
package claude

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"herdr-anywhere/companion/internal/chat"
)

// Reader reads under Dir, Claude Code's config folder (CLAUDE_CONFIG_DIR, else ~/.claude).
type Reader struct {
	Dir string

	mu     sync.Mutex
	cache  map[string]parsed // by path: the last turns read, and the file they were read from
	parses int               // for tests
}

type parsed struct {
	size  int64
	mod   time.Time
	ino   uint64
	turns []chat.Turn
}

var _ chat.Reader = (*Reader)(nil)

var sessionID = regexp.MustCompile(`^[0-9a-fA-F-]{8,64}$`)

// Find: the pid file must name this very process (its pid, and its start time, so a pid used again by another
// program does not match), and exactly one transcript must carry its session id.
func (r *Reader) Find(pid int) (string, string, error) {
	b, err := os.ReadFile(filepath.Join(r.Dir, "sessions", fmt.Sprintf("%d.json", pid)))
	if err != nil {
		return "", "", &chat.Miss{Reason: noFile(pid, chat.NoMatch)}
	}
	var f struct {
		Pid       int    `json:"pid"`
		SessionID string `json:"sessionId"`
		ProcStart string `json:"procStart"`
		Version   string `json:"version"`
	}
	// The first object is the file: Claude Code rewrites it in place, and a shorter write leaves the old end after it.
	if json.NewDecoder(bytes.NewReader(b)).Decode(&f) != nil || f.Pid != pid || !sessionID.MatchString(f.SessionID) {
		return "", "", &chat.Miss{Reason: chat.NoMatch}
	}
	if started, err := startTime(pid); err != nil || started != f.ProcStart {
		return "", "", &chat.Miss{Reason: chat.NoMatch}
	}
	m, _ := filepath.Glob(filepath.Join(r.Dir, "projects", "*", f.SessionID+".jsonl"))
	switch len(m) {
	case 0:
		return "", f.Version, &chat.Miss{Reason: noFile(pid, chat.NoRecordYet)}
	case 1:
		return m[0], f.Version, nil
	}
	return "", f.Version, &chat.Miss{Reason: chat.Ambiguous}
}

// noFile says why a file is missing: a Claude Code that inherited CLAUDE_CODE_CHILD_SESSION=1 saves no transcript
// ("Transcript saving is off", seen with 2.1.319), as when a herdr-core was first started from inside one
// and passes that environment to every terminal. Otherwise, why.
func noFile(pid int, why string) string {
	out, _ := exec.Command("ps", "eww", "-o", "command=", "-p", fmt.Sprint(pid)).Output()
	if bytes.Contains(out, []byte(" CLAUDE_CODE_CHILD_SESSION=1")) {
		return chat.NoRecord
	}
	return why
}

// startTime is how Claude Code writes procStart: ps's lstart in UTC (17 of 17 running sessions of 2.1.319).
func startTime(pid int) (string, error) {
	cmd := exec.Command("ps", "-o", "lstart=", "-p", fmt.Sprint(pid))
	cmd.Env = append(os.Environ(), "TZ=UTC")
	out, err := cmd.Output()
	return strings.TrimSpace(string(out)), err
}

// --- the transcript ------------------------------------------------------------------------------------------

type line struct {
	Type          string          `json:"type"`
	UUID          string          `json:"uuid"`
	Timestamp     string          `json:"timestamp"`
	Cwd           string          `json:"cwd"`
	IsMeta        bool            `json:"isMeta"`
	IsSidechain   bool            `json:"isSidechain"`
	IsCompact     bool            `json:"isCompactSummary"`
	IsAPIError    bool            `json:"isApiErrorMessage"`
	TurnOrigin    string          `json:"turnOrigin"`
	TurnPosition  json.RawMessage `json:"turnPosition"`
	Interrupted   string          `json:"interruptedMessageId"`
	Message       *message        `json:"message"`
	ToolUseResult json.RawMessage `json:"toolUseResult"`
	HasMessageKey bool            `json:"-"`
}

type message struct {
	ID         string          `json:"id"`
	Role       string          `json:"role"`
	Content    json.RawMessage `json:"content"`
	StopReason *string         `json:"stop_reason"`
}

type block struct {
	Type      string          `json:"type"`
	Text      string          `json:"text"`
	ID        string          `json:"id"`
	Name      string          `json:"name"`
	Input     json.RawMessage `json:"input"`
	ToolUseID string          `json:"tool_use_id"`
	Content   json.RawMessage `json:"content"`
	Thinking  string          `json:"thinking"`
	IsError   bool            `json:"is_error"`
	Source    *struct {
		Type      string `json:"type"`
		MediaType string `json:"media_type"`
		Data      string `json:"data"`
	} `json:"source"`
}

// shown says whether an image block goes to the phone as an image.
func (b block) shown() bool {
	return b.Type == "image" && b.Source != nil && b.Source.Type == "base64" && chat.ImageTypes[b.Source.MediaType]
}

var knownBlocks = map[string]bool{"text": true, "thinking": true, "redacted_thinking": true, "tool_use": true, "tool_result": true, "image": true, "document": true}

// Lines with a message: the turns are in user and assistant; api-request-blob is a copy of what was sent.
var withMessage = map[string]bool{"user": true, "assistant": true, "api-request-blob": true}

// A background task's notice (2.1.319): its summary says what happened; the rest is for the model.
// imageSource is the line Claude Code writes after a prompt for each image in it: where it saved the image.
var imageSource = regexp.MustCompile(`\[Image: source: ([^\]\n]+)\]`)

var taskSummary = regexp.MustCompile(`^\s*<task-notification>[\s\S]*?<summary>([^<]*)</summary>`)

// A command typed after "!" in Claude Code, and its output on the line after (which carries the turn mark).
var (
	bashInput  = regexp.MustCompile(`^\s*<bash-input>([\s\S]*)</bash-input>\s*$`)
	bashOutput = regexp.MustCompile(`^\s*(<bash-(stdout|stderr)>[\s\S]*?</bash-(stdout|stderr)>\s*)+$`)
)

// A slash command run by Claude Code itself (/model, /exit…), on a line without a turn mark, and what it printed, on
// the next (2.1.322 writes the output there when interactive, as a system line under -p).
var (
	slashLine   = regexp.MustCompile(`^\s*(<command-(name|message|args)>[\s\S]*?</command-(name|message|args)>\s*)+$`)
	slashName   = regexp.MustCompile(`<command-name>(/[^<]+)</command-name>`)
	slashArgs   = regexp.MustCompile(`<command-args>([\s\S]*?)</command-args>`)
	slashOutput = regexp.MustCompile(`^\s*(<local-command-(stdout|stderr)>[\s\S]*?</local-command-(stdout|stderr)>\s*)+$`)
)

var stops = map[string]bool{"end_turn": true, "stop_sequence": true, "max_tokens": true, "tool_use": true}

// lines reads the file's last MaxTail bytes as whole lines. from0 says the window starts at the file's start.

func parse(raw []byte) (line, error) {
	var l line
	if err := json.Unmarshal(raw, &l); err != nil {
		return l, chat.Formatf("a line that is not JSON")
	}
	var keys map[string]json.RawMessage
	json.Unmarshal(raw, &keys)
	_, l.HasMessageKey = keys["message"]
	return l, nil
}

func blocks(m *message) ([]block, string, error) {
	var s string
	if json.Unmarshal(m.Content, &s) == nil {
		return nil, s, nil
	}
	var bs []block
	if err := json.Unmarshal(m.Content, &bs); err != nil {
		return nil, "", chat.Formatf("message.content")
	}
	for _, b := range bs {
		if !knownBlocks[b.Type] {
			return nil, "", chat.Formatf("content block %q", b.Type)
		}
	}
	return bs, "", nil
}

type building struct {
	t      chat.Turn
	start  time.Time
	last   time.Time
	cwd    string
	texts  []string // assistant text since the last tool call: the reply, if nothing follows
	textID string
	asked  map[string]*chat.Ask // open AskUserQuestion calls by tool id
}

// sources gives the turn's images, in order, the paths a meta line names for them.
func (b *building) sources(m *message) {
	bs, str, err := blocks(m)
	if err != nil {
		return
	}
	text := str
	for _, x := range bs {
		if x.Type == "text" {
			text += "\n" + x.Text
		}
	}
	next := 0
	for _, m := range imageSource.FindAllStringSubmatch(text, -1) {
		for next < len(b.t.Images) && b.t.Images[next].Source != "" {
			next++
		}
		if next == len(b.t.Images) {
			return
		}
		b.t.Images[next].Source = strings.TrimSpace(m[1])
	}
}

func (b *building) flushNotes() {
	if len(b.texts) == 0 {
		return
	}
	first := strings.TrimSpace(strings.SplitN(strings.Join(b.texts, "\n\n"), "\n", 2)[0])
	b.t.Steps = append(b.t.Steps, chat.Step{ID: b.textID, Kind: "note", Title: title(first)})
	b.texts, b.textID = nil, ""
}

func (b *building) finish() chat.Turn {
	t := b.t
	t.Reply = strings.Join(b.texts, "\n\n")
	for _, a := range b.asked {
		if t.End == "open" {
			t.Ask = a
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

// Turns reads the transcript's end, again only when the file changed (its size, time or inode): the phone asks every
// 3 s while the agent works.
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

// parse reads the transcript's end. A turn the window starts in the middle of is left out.
func (r *Reader) parse(path string) ([]chat.Turn, error) {
	r.parses++
	ls, from0, err := chat.TailLines(path)
	if err != nil {
		return nil, err
	}
	var out []chat.Turn
	var cur *building
	partial := !from0 // lines before the first prompt belong to a turn that began before the window
	for _, raw := range ls {
		l, err := parse(raw)
		if err != nil {
			return nil, err
		}
		if l.HasMessageKey && !withMessage[l.Type] {
			return nil, chat.Formatf("a %q line with a message", l.Type)
		}
		if l.IsMeta && l.Type == "user" && cur != nil && l.Message != nil {
			cur.sources(l.Message)
		}
		if !withMessage[l.Type] || l.Type == "api-request-blob" || l.IsSidechain || l.IsMeta || l.IsCompact {
			continue
		}
		if l.Message == nil {
			return nil, chat.Formatf("%s line without message", l.Type)
		}
		bs, str, err := blocks(l.Message)
		if err != nil {
			return nil, err
		}
		if l.Type == "user" {
			text, results := str, 0
			var parts []string
			var images []chat.Image
			for i, b := range bs {
				switch {
				case b.Type == "text":
					parts = append(parts, b.Text)
				case b.shown():
					images = append(images, chat.Image{ID: fmt.Sprintf("%s.%d", l.UUID, i)})
				case b.Type == "image" || b.Type == "document":
					parts = append(parts, "["+b.Type+"]")
				case b.Type == "tool_result":
					results++
				}
			}
			if bs != nil {
				text = strings.Join(parts, "\n")
			}
			if results > 0 {
				if cur != nil {
					for _, b := range bs {
						if b.Type == "tool_result" {
							// What the result says of its step: failed, or the lines an edit changed.
							for i := range cur.t.Steps {
								if s := &cur.t.Steps[i]; s.ID == b.ToolUseID {
									s.Failed = b.IsError
									if s.Kind == "edit" {
										if _, c, _ := shaped("Edit", nil, l.ToolUseResult, b.IsError, ""); c != nil {
											s.Added, s.Deleted = c.Added, c.Deleted
										}
									}
								}
							}
							if a, ok := cur.asked[b.ToolUseID]; ok {
								cur.t.Steps = append(cur.t.Steps, chat.Step{ID: b.ToolUseID, Kind: "other", Title: title(a.Text)})
								delete(cur.asked, b.ToolUseID)
							}
						}
					}
				}
				continue
			}
			// begin ends the turn being built and starts one.
			begin := func(from, said, end string) {
				if cur != nil {
					out = append(out, cur.finish())
				}
				partial = false
				cur = &building{t: chat.Turn{ID: l.UUID, From: from, Said: strings.TrimSpace(said), At: l.Timestamp, Images: images, End: end}, start: when(l.Timestamp), cwd: l.Cwd, asked: map[string]*chat.Ask{}}
			}
			marked := len(l.TurnPosition) > 0 && string(l.TurnPosition) != "null"
			switch {
			case bashOutput.MatchString(text):
				// A "!" command's output: it stays in the terminal.
			case !marked && bashInput.MatchString(text):
				// A command typed after "!": the shell runs it, no reply follows.
				begin("you", "! "+strings.TrimSpace(bashInput.FindStringSubmatch(text)[1]), "done")
			case !marked && slashOutput.MatchString(text):
				// What a slash command printed: it stays in the terminal.
			case !marked && slashLine.MatchString(text) && slashName.MatchString(text):
				said := slashName.FindStringSubmatch(text)[1]
				if a := slashArgs.FindStringSubmatch(text); a != nil && strings.TrimSpace(a[1]) != "" {
					said += " " + strings.TrimSpace(a[1])
				}
				begin("you", said, "done")
			case !marked && taskSummary.MatchString(text):
				// A background task's notice written only to the record, while the user typed.
				begin("other", taskSummary.FindStringSubmatch(text)[1], "done")
			case marked:
				from := "other"
				if l.TurnOrigin == "human" || l.TurnOrigin == "sdk" {
					from = "you"
				} else if m := taskSummary.FindStringSubmatch(text); m != nil {
					text = m[1]
				}
				begin(from, text, "open")
			case strings.HasPrefix(strings.TrimSpace(text), "[Request interrupted by user"):
				if cur != nil {
					cur.t.End = "stopped"
					cur.asked = map[string]*chat.Ask{}
				}
			default:
				return nil, chat.Formatf("a user text line that is neither a prompt nor an interruption")
			}
			continue
		}
		// assistant
		if partial || cur == nil {
			continue
		}
		var stop string
		if l.Message.StopReason != nil {
			stop = *l.Message.StopReason
			if !stops[stop] {
				return nil, chat.Formatf("stop_reason %q", stop)
			}
		}
		for _, b := range bs {
			switch b.Type {
			case "text":
				if strings.TrimSpace(b.Text) != "" {
					if cur.textID == "" {
						cur.textID = l.UUID
					}
					cur.texts = append(cur.texts, b.Text)
				}
			case "thinking":
				// Most thinking is recorded without its text (only a signature); what has text is a step.
				if s := strings.TrimSpace(b.Thinking); s != "" {
					cur.flushNotes()
					cur.t.Steps = append(cur.t.Steps, chat.Step{ID: l.UUID, Kind: "think", Title: title(s)})
				}
			case "tool_use":
				if b.ID == "" || b.Name == "" {
					return nil, chat.Formatf("tool_use without id or name")
				}
				cur.flushNotes()
				if b.Name == "AskUserQuestion" {
					cur.asked[b.ID] = question(b.Input)
					continue
				}
				kind, name := describe(b.Name, b.Input, cur.cwd)
				cur.t.Steps = append(cur.t.Steps, chat.Step{ID: b.ID, Kind: kind, Title: title(name)})
			}
		}
		cur.last = when(l.Timestamp)
		if l.IsAPIError {
			cur.t.End = "stopped"
		} else if stop != "" && stop != "tool_use" && cur.t.End == "open" {
			cur.t.End = "done"
		}
	}
	if cur != nil {
		out = append(out, cur.finish())
	}
	if len(out) > chat.MaxTurns {
		out = out[len(out)-chat.MaxTurns:]
	}
	return out, nil
}

func question(in json.RawMessage) *chat.Ask {
	var q struct {
		Questions []struct {
			Question string `json:"question"`
			Options  []struct {
				Label string `json:"label"`
			} `json:"options"`
		} `json:"questions"`
	}
	a := &chat.Ask{Options: []string{}}
	if json.Unmarshal(in, &q) == nil && len(q.Questions) > 0 {
		a.Text = q.Questions[0].Question
		for _, o := range q.Questions[0].Options {
			a.Options = append(a.Options, o.Label)
		}
	}
	return a
}

// title is a step's line: the first line, at most 80 bytes. Details come only through Detail.
func title(s string) string {
	return chat.Cut(strings.TrimSpace(strings.SplitN(strings.TrimSpace(s), "\n", 2)[0]), 80)
}

// describe gives a tool call its kind and a short title. A tool it doesn't know is "other", titled with its name.
func describe(name string, in json.RawMessage, cwd string) (string, string) {
	var f map[string]any
	json.Unmarshal(in, &f)
	str := func(k string) string { s, _ := f[k].(string); return strings.TrimSpace(s) }
	path := func() string {
		p := str("file_path")
		if p == "" {
			p = str("notebook_path")
		}
		if cwd != "" && strings.HasPrefix(p, strings.TrimRight(cwd, "/")+"/") {
			return p[len(strings.TrimRight(cwd, "/"))+1:]
		}
		return p
	}
	or := func(a, b string) string {
		if a != "" {
			return a
		}
		return b
	}
	firstLine := func(s string) string { return strings.SplitN(s, "\n", 2)[0] }
	switch name {
	case "Read":
		return "read", or(path(), name)
	case "Edit", "Write", "MultiEdit", "NotebookEdit":
		return "edit", or(path(), name)
	case "Bash":
		return "run", or(firstLine(str("command")), str("description"))
	case "Grep", "Glob":
		return "search", or(str("pattern"), name)
	case "WebFetch":
		return "web", or(str("url"), name)
	case "WebSearch":
		return "web", or(str("query"), name)
	case "Agent", "Task":
		return "agent", or(str("description"), name)
	}
	if strings.HasPrefix(name, "mcp__") {
		if i := strings.LastIndex(name, "__"); i > 4 {
			return "other", name[i+2:]
		}
	}
	return "other", name
}

// Detail finds a step again: a tool call's input and result, or a note's whole text.
func (r *Reader) Detail(path, step string) (chat.Detail, error) {
	ls, _, err := chat.TailLines(path)
	if err != nil {
		return chat.Detail{}, err
	}
	var d chat.Detail
	found, read := false, "" // read: the file the step's tool read, where its result's images came from
	tool, input := "", json.RawMessage(nil)
	for _, raw := range ls {
		l, err := parse(raw)
		if err != nil || l.Message == nil || (l.Type != "user" && l.Type != "assistant") {
			continue
		}
		bs, _, err := blocks(l.Message)
		if err != nil {
			continue
		}
		for _, b := range bs {
			switch {
			case l.Type == "assistant" && b.Type == "tool_use" && b.ID == step:
				var in struct {
					FilePath string `json:"file_path"`
				}
				if json.Unmarshal(b.Input, &in) == nil {
					read = in.FilePath
				}
				tool, input = b.Name, b.Input
				var pretty bytes.Buffer
				if json.Indent(&pretty, b.Input, "", "  ") != nil {
					pretty.Write(b.Input)
				}
				d.Input, found = chat.Cut(pretty.String(), chat.MaxDetail), true
			case l.Type == "assistant" && b.Type == "text" && l.UUID == step:
				d.Input, found = chat.Cut(d.Input+b.Text, chat.MaxDetail), true
			case l.Type == "assistant" && b.Type == "thinking" && l.UUID == step:
				d.Input, found = chat.Cut(b.Thinking, chat.MaxDetail), true
			case l.Type == "user" && b.Type == "tool_result" && b.ToolUseID == step:
				d.Output = chat.Cut(resultText(b.Content), chat.MaxDetail)
				d.Run, d.Change, d.Read = shaped(tool, input, l.ToolUseResult, b.IsError, resultText(b.Content))
				for i, x := range resultBlocks(b.Content) {
					if x.shown() {
						d.Images = append(d.Images, chat.Image{ID: fmt.Sprintf("%s.%d", step, i)})
					}
				}
			}
		}
	}
	for i := range d.Images {
		d.Images[i].Source = read
	}
	if !found {
		return chat.Detail{}, chat.ErrNoStep
	}
	return d, nil
}

func resultBlocks(c json.RawMessage) []block {
	var bs []block
	json.Unmarshal(c, &bs)
	return bs
}

func resultText(c json.RawMessage) string {
	var s string
	if json.Unmarshal(c, &s) == nil {
		return s
	}
	var parts []string
	for _, b := range resultBlocks(c) {
		switch {
		case b.Type == "text":
			parts = append(parts, b.Text)
		case b.Type == "image" && !b.shown():
			parts = append(parts, "[image]")
		}
	}
	return strings.Join(parts, "\n")
}

// Image finds an image again: "<prompt line uuid>.<block>" or "<tool call id>.<block of its result>".
func (r *Reader) Image(path, id string) (string, []byte, error) {
	i := strings.LastIndexByte(id, '.')
	if i <= 0 {
		return "", nil, chat.ErrNoStep
	}
	ref := id[:i]
	n, err := strconv.Atoi(id[i+1:])
	if err != nil || n < 0 {
		return "", nil, chat.ErrNoStep
	}
	ls, _, err := chat.TailLines(path)
	if err != nil {
		return "", nil, err
	}
	for _, raw := range ls {
		l, err := parse(raw)
		if err != nil || l.Type != "user" || l.Message == nil {
			continue
		}
		bs, _, err := blocks(l.Message)
		if err != nil {
			continue
		}
		var in []block
		if l.UUID == ref {
			in = bs
		} else {
			for _, b := range bs {
				if b.Type == "tool_result" && b.ToolUseID == ref {
					in = resultBlocks(b.Content)
				}
			}
		}
		if n < len(in) && in[n].shown() {
			data, err := base64.StdEncoding.DecodeString(in[n].Source.Data)
			if err != nil {
				return "", nil, chat.ErrNoStep
			}
			return in[n].Source.MediaType, data, nil
		}
	}
	return "", nil, chat.ErrNoStep
}

// exitCode: how Claude Code starts a failed command's result ("Exit code 3\n<what it printed>").
var exitCode = regexp.MustCompile(`^(?:Error: )?Exit code (\d+)\n?`)

// shaped reads a tool call and its result as the phone shows them: a command, a change to a file, the lines read.
// A command comes from its result's text (the record has it even when toolUseResult is missing); a change and a
// read only from toolUseResult, which says the lines and where they are.
func shaped(tool string, input, res json.RawMessage, failed bool, text string) (*chat.Run, *chat.Change, *chat.Read) {
	switch tool {
	case "Bash":
		var in struct {
			Command string `json:"command"`
		}
		json.Unmarshal(input, &in)
		r := &chat.Run{Command: in.Command, Failed: failed}
		out := text
		if failed {
			if m := exitCode.FindStringSubmatch(out); m != nil {
				r.Code, _ = strconv.Atoi(m[1])
				out = out[len(m[0]):]
			}
		}
		out = strings.TrimRight(out, "\n")
		if out != "" {
			r.Lines = strings.Count(out, "\n") + 1
		}
		r.Output = chat.CutEnd(out, chat.MaxDetail)
		return r, nil, nil
	case "Edit", "Write", "MultiEdit":
		var u struct {
			Type     string `json:"type"`
			FilePath string `json:"filePath"`
			Content  string `json:"content"`
			Patch    []struct {
				OldStart int      `json:"oldStart"`
				NewStart int      `json:"newStart"`
				Lines    []string `json:"lines"`
			} `json:"structuredPatch"`
		}
		if failed || json.Unmarshal(res, &u) != nil || u.FilePath == "" {
			return nil, nil, nil
		}
		c := &chat.Change{File: u.FilePath, Hunks: []chat.Hunk{}}
		if len(u.Patch) == 0 && u.Type == "create" {
			var lines []string
			for _, x := range strings.Split(strings.TrimSuffix(u.Content, "\n"), "\n") {
				lines = append(lines, "+"+x)
			}
			u.Patch = append(u.Patch, struct {
				OldStart int      `json:"oldStart"`
				NewStart int      `json:"newStart"`
				Lines    []string `json:"lines"`
			}{0, 1, lines})
		}
		n := 0
		for _, h := range u.Patch {
			hk := chat.Hunk{OldStart: h.OldStart, NewStart: h.NewStart, Lines: []string{}}
			for _, x := range h.Lines {
				switch {
				case strings.HasPrefix(x, "+"):
					c.Added++
				case strings.HasPrefix(x, "-"):
					c.Deleted++
				}
				if n < chat.MaxChangeLines {
					hk.Lines = append(hk.Lines, x)
				} else {
					c.Cut = true
				}
				n++
			}
			if len(hk.Lines) > 0 {
				c.Hunks = append(c.Hunks, hk)
			}
		}
		return nil, c, nil
	case "Read":
		var u struct {
			Type string `json:"type"`
			File struct {
				FilePath   string `json:"filePath"`
				StartLine  int    `json:"startLine"`
				NumLines   int    `json:"numLines"`
				TotalLines int    `json:"totalLines"`
			} `json:"file"`
		}
		if failed || json.Unmarshal(res, &u) != nil || u.Type != "text" || u.File.FilePath == "" {
			return nil, nil, nil
		}
		return nil, nil, &chat.Read{File: u.File.FilePath, From: u.File.StartLine, Count: u.File.NumLines, Total: u.File.TotalLines}
	}
	return nil, nil, nil
}

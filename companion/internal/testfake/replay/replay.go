// Package replay writes an agent's recorded turns (companion/internal/chat/<agent>/testdata, each made by the agent
// itself against a fake API) into a record where that agent keeps it, the way it writes it, so the Companion's
// readers find and read it as the agent's own: Claude Code's transcript and pid file, Codex's rollout file (held
// open, as Codex holds it), Pi's conversation file, OpenCode 2's opencode.db. The e2e's fake agents (fakeagent) are
// built on it; Go, so they run on every runner, with no agent installed.
//
// Each turn played gets new ids (a turn played twice is two turns, its steps found again by their own ids), the
// recording's folder becomes the pane's, and the prompt becomes what was typed. Preload plays a long conversation
// in at once, its prompts numbered "001 …". Long puts that many invented lines in place of what the recordings'
// LIST command printed. Fake data only: the recordings' and these.
package replay

import (
	"bytes"
	"crypto/rand"
	"database/sql"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"
)

// The folder the recordings were made in, and what their LIST command printed.
const (
	recorded = "/work/acme"
	listed   = "notes.txt\nshot.png\ntodo.txt"
)

// idKeys: the keys whose string values are ids, in any agent's lines; every other string equal to one of them (a
// parent, a tool result's call) is the same id.
var idKeys = map[string]bool{"uuid": true, "id": true, "turn_id": true, "root_turn_id": true, "itemId": true}

type Script struct {
	agent   string
	version string
	head    []any   // what comes before the first turn
	turns   [][]any // each turn's lines (OpenCode: its messages)
	person  []bool  // the turn starts with what a person typed (not a notice another program started)
	prompt  []string
	schema  string // OpenCode: its tables
	title   string // OpenCode: the session's title
}

// Load reads the recordings of one agent ("claude-code", "codex", "pi" or "opencode"), in order.
func Load(agent string, fixtures ...string) (*Script, error) {
	if len(fixtures) == 0 {
		return nil, fmt.Errorf("replay: no recording")
	}
	s := &Script{agent: agent}
	switch agent {
	case "claude-code":
		s.version = filepath.Base(filepath.Dir(fixtures[0]))
	case "opencode":
		b, err := os.ReadFile(filepath.Join(filepath.Dir(fixtures[0]), "schema.sql"))
		if err != nil {
			return nil, err
		}
		s.schema = string(b)
	case "codex", "pi":
	default:
		return nil, fmt.Errorf("replay: no agent %q", agent)
	}
	for _, f := range fixtures {
		lines, err := read(agent, f, s)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", f, err)
		}
		begun := false // this recording's first turn has begun
		for _, d := range lines {
			if starts(agent, d) {
				begun = true
				s.turns = append(s.turns, []any{d})
				s.person = append(s.person, person(agent, d))
				s.prompt = append(s.prompt, "")
			} else if begun {
				s.turns[len(s.turns)-1] = append(s.turns[len(s.turns)-1], d)
			} else if f == fixtures[0] {
				// Each recording has its own head (Codex's session_meta, Pi's session line): only the first one's is
				// written, the others' lines before their first turn are dropped.
				s.head = append(s.head, d)
			}
		}
	}
	if len(s.turns) == 0 {
		return nil, fmt.Errorf("replay: no turns in %v", fixtures)
	}
	for i, t := range s.turns {
		s.prompt[i] = prompt(agent, t)
	}
	if agent == "codex" {
		for _, d := range s.head {
			if p := obj(obj(d)["payload"]); p != nil && p["cli_version"] != nil {
				s.version, _ = p["cli_version"].(string)
			}
		}
	}
	return s, nil
}

// read: a recording's lines (JSON Lines), or OpenCode's messages (its fixture's JSON, sessions and messages).
func read(agent, file string, s *Script) ([]any, error) {
	b, err := os.ReadFile(file)
	if err != nil {
		return nil, err
	}
	if agent == "opencode" {
		var f struct {
			Sessions []struct {
				Title   string `json:"title"`
				Version string `json:"version"`
			} `json:"sessions"`
			Messages []json.RawMessage `json:"messages"`
		}
		if err := json.Unmarshal(b, &f); err != nil {
			return nil, err
		}
		if s.version == "" && len(f.Sessions) > 0 {
			s.version, s.title = f.Sessions[0].Version, f.Sessions[0].Title
		}
		var out []any
		for _, m := range f.Messages {
			v, err := decode(m)
			if err != nil {
				return nil, err
			}
			out = append(out, v)
		}
		return out, nil
	}
	var out []any
	for _, l := range bytes.Split(b, []byte("\n")) {
		if len(bytes.TrimSpace(l)) == 0 {
			continue
		}
		v, err := decode(l)
		if err != nil {
			return nil, err
		}
		out = append(out, v)
	}
	return out, nil
}

func decode(b []byte) (any, error) {
	d := json.NewDecoder(bytes.NewReader(b))
	d.UseNumber()
	var v any
	err := d.Decode(&v)
	return v, err
}

func obj(v any) map[string]any { m, _ := v.(map[string]any); return m }

// starts: the line begins a turn.
func starts(agent string, d any) bool {
	m := obj(d)
	switch agent {
	case "claude-code":
		return m["type"] == "user" && m["turnPosition"] != nil
	case "codex":
		return m["type"] == "event_msg" && obj(m["payload"])["type"] == "task_started"
	case "pi":
		return m["type"] == "message" && obj(m["message"])["role"] == "user"
	case "opencode":
		return m["type"] == "user"
	}
	return false
}

func person(agent string, d any) bool {
	if agent != "claude-code" {
		return true
	}
	o := obj(d)["turnOrigin"]
	return o == "human" || o == "sdk"
}

// prompt: what the person typed to start the turn, as the record has it.
func prompt(agent string, t []any) string {
	text := func(c any) string {
		if s, ok := c.(string); ok {
			return s
		}
		for _, b := range asList(c) {
			if bm := obj(b); bm != nil && (bm["type"] == "text" || bm["type"] == "input_text") {
				s, _ := bm["text"].(string)
				return s
			}
		}
		return ""
	}
	switch agent {
	case "claude-code":
		return text(obj(obj(t[0])["message"])["content"])
	case "pi":
		return text(obj(obj(t[0])["message"])["content"])
	case "opencode":
		s, _ := obj(obj(t[0])["data"])["text"].(string)
		return s
	case "codex":
		for _, d := range t {
			p := obj(obj(d)["payload"])
			if it := obj(p["item"]); p["type"] == "item_completed" && it["type"] == "UserMessage" {
				return text(it["content"])
			}
		}
	}
	return ""
}

func asList(v any) []any { l, _ := v.([]any); return l }

// Options: where the record goes and whose it is.
type Options struct {
	// Dir is the agent's own folder: CLAUDE_CONFIG_DIR, CODEX_HOME, Pi's agent folder (PI_CODING_AGENT_DIR), or
	// OpenCode's data home (XDG_DATA_HOME; the database is opencode/opencode.db in it).
	Dir string
	Cwd string // the pane's folder, in place of the recording's
	Pid int    // Claude Code: the pid its pid file names
	// ProcStart: Claude Code's pid file names when the process started (ps's lstart, in UTC).
	ProcStart string
	// Long: this many invented lines in place of what the recordings' LIST command printed; 0 keeps it.
	Long int
	// Now is the clock the lines are stamped with (default time.Now).
	Now func() time.Time
}

type Player struct {
	s     *Script
	o     Options
	path  string
	file  *os.File
	db    *sql.DB
	sid   string
	names map[string]string // ids renamed for the whole record (the session's, Codex's thread)
	last  string            // the last line's id: the next turn's parent (Claude Code, Pi)
	head  bool              // the head is written
	next  int               // the next turn to play
	plays int
	seq   int64 // OpenCode's message order; Codex's line ordinal
}

func uuid() string {
	var b [16]byte
	rand.Read(b[:])
	b[6] = b[6]&0x0f | 0x40
	b[8] = b[8]&0x3f | 0x80
	return fmt.Sprintf("%x-%x-%x-%x-%x", b[0:4], b[4:6], b[6:8], b[8:10], b[10:])
}

// Start makes the record the agent would have at its start (Claude Code: only its pid file; its transcript comes with
// the first turn, as Claude Code's does).
func (s *Script) Start(o Options) (*Player, error) {
	if o.Now == nil {
		o.Now = time.Now
	}
	p := &Player{s: s, o: o, sid: uuid(), names: map[string]string{}}
	now := o.Now().UTC()
	var err error
	switch s.agent {
	case "claude-code":
		dir := filepath.Join(o.Dir, "projects", strings.NewReplacer("/", "-", ".", "-").Replace(o.Cwd))
		p.path = filepath.Join(dir, p.sid+".jsonl")
		if err = os.MkdirAll(filepath.Join(o.Dir, "sessions"), 0o755); err == nil {
			err = os.MkdirAll(dir, 0o755)
		}
		if err == nil {
			pf, _ := json.Marshal(map[string]any{"pid": o.Pid, "sessionId": p.sid, "cwd": o.Cwd, "procStart": o.ProcStart,
				"version": s.version, "kind": "interactive", "entrypoint": "cli"})
			err = os.WriteFile(filepath.Join(o.Dir, "sessions", strconv.Itoa(o.Pid)+".json"), pf, 0o644)
		}
	case "codex":
		for _, d := range s.head {
			if pl := obj(obj(d)["payload"]); obj(d)["type"] == "session_meta" {
				if id, _ := pl["id"].(string); id != "" {
					p.names[id] = p.sid
				}
			}
		}
		dir := filepath.Join(o.Dir, "sessions", now.Format("2006"), now.Format("01"), now.Format("02"))
		p.path = filepath.Join(dir, "rollout-"+now.Format("2006-01-02T15-04-05")+"-"+p.sid+".jsonl")
		err = p.open(dir)
	case "pi":
		for _, d := range s.head {
			if obj(d)["type"] == "session" {
				if id, _ := obj(d)["id"].(string); id != "" {
					p.names[id] = p.sid
				}
			}
		}
		dir := filepath.Join(o.Dir, "sessions", "--"+strings.ReplaceAll(strings.Trim(o.Cwd, "/"), "/", "-")+"--")
		p.path = filepath.Join(dir, now.Format("2006-01-02T15-04-05-000Z")+"_"+p.sid+".jsonl")
		err = p.open(dir)
	case "opencode":
		p.sid = "ses_" + strings.ReplaceAll(uuid(), "-", "")[:26]
		dir := filepath.Join(o.Dir, "opencode")
		p.path = filepath.Join(dir, "opencode.db") + "#" + p.sid
		if err = os.MkdirAll(dir, 0o755); err != nil {
			break
		}
		if p.db, err = sql.Open("sqlite", filepath.Join(dir, "opencode.db")); err != nil {
			break
		}
		p.db.SetMaxOpenConns(1)
		if _, err = p.db.Exec("PRAGMA journal_mode=WAL;" + s.schema); err != nil {
			break
		}
		ms := now.UnixMilli()
		_, err = p.db.Exec("INSERT INTO session_v2 (id, project_id, parent_id, slug, directory, title, version, time_created, time_updated) VALUES (?, 'demo', NULL, 'demo', ?, ?, ?, ?, ?)",
			p.sid, o.Cwd, s.title, s.version, ms, ms)
	}
	if err != nil {
		p.Close()
		return nil, err
	}
	if s.agent != "claude-code" && s.agent != "opencode" {
		for _, d := range s.head {
			if err := p.write(p.fix(d, nil, nil)); err != nil {
				p.Close()
				return nil, err
			}
		}
		p.head = true
	}
	return p, nil
}

func (p *Player) open(dir string) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	f, err := os.OpenFile(p.path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o644)
	p.file = f
	return err
}

// Path is the record as the agent's reader names it (OpenCode: the database, # and the session).
func (p *Player) Path() string { return p.path }

// Session is the record's session id (OpenCode reports it to herdr-core while it works).
func (p *Player) Session() string { return p.sid }

func (p *Player) Close() {
	if p.file != nil {
		p.file.Close()
	}
	if p.db != nil {
		p.db.Close()
	}
}

// Preload plays n turns in at once, each prompt numbered: "001 LIST the files here".
func (p *Player) Preload(n int) error {
	for i := 1; i <= n; i++ {
		said := fmt.Sprintf("%03d", i)
		if pr := p.s.prompt[p.next%len(p.s.turns)]; pr != "" {
			said += " " + pr
		}
		if err := p.Play(said, 0); err != nil {
			return err
		}
	}
	return nil
}

// Play writes the next recorded turn with said as its prompt, delay apart line by line, then the turns another
// program starts right after it (Claude Code's notices).
func (p *Player) Play(said string, delay time.Duration) error {
	if !p.head {
		for _, d := range p.s.head {
			if err := p.write(p.fix(d, nil, nil)); err != nil {
				return err
			}
		}
		p.head = true
	}
	if err := p.turn(p.next%len(p.s.turns), &said, delay); err != nil {
		return err
	}
	p.next++
	for !p.s.person[p.next%len(p.s.turns)] {
		if err := p.turn(p.next%len(p.s.turns), nil, delay); err != nil {
			return err
		}
		p.next++
	}
	return nil
}

// PlayFor plays the recorded turn whose prompt is said, else the first whose prompt starts with said's first word
// (LIST, EDIT, BREAK, LOOK…: the words the fake API answers), else the next one; with said as its prompt.
func (p *Player) PlayFor(said string, delay time.Duration) error {
	word := func(s string) string {
		return strings.Trim(strings.SplitN(strings.TrimSpace(s)+" ", " ", 2)[0], ",.!?:")
	}
	pick := -1
	for i, pr := range p.s.prompt {
		if pr == said && p.s.person[i] {
			pick = i
			break
		}
	}
	for i, pr := range p.s.prompt {
		if pick < 0 && p.s.person[i] && word(pr) != "" && word(pr) == word(said) {
			pick = i
		}
	}
	if pick >= 0 {
		p.next = pick
	}
	return p.Play(said, delay)
}

func (p *Player) turn(i int, said *string, delay time.Duration) error {
	p.plays++
	ren := map[string]string{}
	for _, d := range p.s.turns[i] {
		ids(d, ren, fmt.Sprintf("-p%d", p.plays))
	}
	var swap map[string]string
	if said != nil && p.s.prompt[i] != "" {
		swap = map[string]string{p.s.prompt[i]: *said}
	}
	for k, d := range p.s.turns[i] {
		v := p.fix(d, ren, swap)
		if k == 0 {
			p.parent(v)
		}
		if err := p.write(v); err != nil {
			return err
		}
		if delay > 0 {
			time.Sleep(delay)
		}
	}
	return nil
}

// ids: every id in v (under idKeys), renamed with the suffix.
func ids(v any, ren map[string]string, suffix string) {
	switch x := v.(type) {
	case map[string]any:
		for k, e := range x {
			if s, ok := e.(string); ok && idKeys[k] && s != "" {
				ren[s] = s + suffix
			} else {
				ids(e, ren, suffix)
			}
		}
	case []any:
		for _, e := range x {
			ids(e, ren, suffix)
		}
	}
}

// fix: a copy of the line for this record: ids renamed, the pane's folder, the typed prompt, the long output.
func (p *Player) fix(v any, ren, swap map[string]string) any {
	var walk func(any) any
	walk = func(v any) any {
		switch x := v.(type) {
		case map[string]any:
			m := make(map[string]any, len(x))
			for k, e := range x {
				m[k] = walk(e)
			}
			return m
		case []any:
			l := make([]any, len(x))
			for i, e := range x {
				l[i] = walk(e)
			}
			return l
		case string:
			if n, ok := p.names[x]; ok {
				return n
			}
			if n, ok := ren[x]; ok {
				return n
			}
			if n, ok := swap[x]; ok {
				return n
			}
			if p.o.Long > 0 && strings.Contains(x, listed) {
				x = strings.ReplaceAll(x, listed, strings.TrimSuffix(long(p.o.Long), "\n"))
			}
			return strings.ReplaceAll(x, recorded, p.o.Cwd)
		}
		return v
	}
	return walk(v)
}

// long: n invented lines.
func long(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "line %d of the long listing: build/out/part-%05d.o\n", i, i)
	}
	return b.String()
}

// parent: a turn's first line follows the last line written (Claude Code and Pi chain their lines).
func (p *Player) parent(v any) {
	m := obj(v)
	if p.last == "" || m == nil {
		return
	}
	switch p.s.agent {
	case "claude-code":
		if _, ok := m["parentUuid"]; ok {
			m["parentUuid"] = p.last
		}
	case "pi":
		if _, ok := m["parentId"]; ok {
			m["parentId"] = p.last
		}
	}
}

// write: one line as the agent writes it, stamped now.
func (p *Player) write(v any) error {
	m := obj(v)
	now := p.o.Now().UTC()
	stamp := now.Format("2006-01-02T15:04:05.000Z")
	switch p.s.agent {
	case "claude-code":
		if _, ok := m["sessionId"]; ok {
			m["sessionId"] = p.sid
		}
		if _, ok := m["timestamp"]; ok {
			m["timestamp"] = stamp
		}
		if id, _ := m["uuid"].(string); id != "" {
			p.last = id
		}
		if p.file == nil {
			if err := p.open(filepath.Dir(p.path)); err != nil {
				return err
			}
		}
	case "codex":
		p.seq++
		m["ordinal"] = p.seq
		m["timestamp"] = stamp
	case "pi":
		if _, ok := m["timestamp"]; ok {
			m["timestamp"] = stamp
		}
		if msg := obj(m["message"]); msg != nil {
			if _, ok := msg["timestamp"]; ok {
				msg["timestamp"] = now.UnixMilli()
			}
		}
		if id, _ := m["id"].(string); id != "" {
			p.last = id
		}
	case "opencode":
		p.seq++
		data, err := marshal(m["data"])
		if err != nil {
			return err
		}
		ms := now.UnixMilli()
		_, err = p.db.Exec("INSERT INTO session_message (id, session_id, type, seq, time_created, time_updated, data) VALUES (?, ?, ?, ?, ?, ?, ?)",
			m["id"], p.sid, m["type"], p.seq, ms, ms, string(data))
		return err
	}
	b, err := marshal(v)
	if err != nil {
		return err
	}
	_, err = p.file.Write(append(b, '\n'))
	return err
}

func marshal(v any) ([]byte, error) {
	var b bytes.Buffer
	e := json.NewEncoder(&b)
	e.SetEscapeHTML(false)
	if err := e.Encode(v); err != nil {
		return nil, err
	}
	return bytes.TrimRight(b.Bytes(), "\n"), nil
}

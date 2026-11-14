package opencode

// Measured with OpenCode 2.2.0 in a container against the fake Responses API (testdata holds what it wrote): the
// sessions are rows of `session_v2` and their messages rows of `session_message`, in opencode.db under OpenCode's data
// folder, kept in WAL mode. While it works OpenCode reports the session id to herdr-core (OSC 7501); at the end of a
// turn it clears it, so the Service remembers it (SessionReporter). Its terminal title is `OC | ` and the session's
// title, cut past 40 UTF-16 units to 37 and "…"; `OpenCode` on its home screen and for an untitled session.
// The title rule as its own code has it (2.2.0's bundled tui: `OC | ${t.length>40?t.slice(0,37)+"\u2026":t}`, `OpenCode`
// for home or a fallback title); Heeler reads 2.0.32 the same way.

import (
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"unicode/utf16"

	_ "modernc.org/sqlite"

	"herdr-anywhere/companion/internal/chat"
)

// Reader reads OpenCode's sessions from its database: OpenCode 2's, and OpenCode 1's (parse1.go).
type Reader struct {
	// DB is opencode.db; empty: under $XDG_DATA_HOME, else ~/.local/share.
	DB string

	mu    sync.Mutex
	cache map[string]cached
}

type cached struct {
	stamp string
	turns []chat.Turn
}

var _ chat.SessionReporter = (*Reader)(nil)

func (r *Reader) ReportsSession() {}

var _ chat.Named = (*Reader)(nil)

// ProgramName: OpenCode 1 reports no status to herdr-core (hc get: status null, foreground name "opencode", measured
// with 1.19.0); its terminal is known by the program's name.
func (r *Reader) ProgramName() string { return "opencode" }

// Find isn't used: an OpenCode's session is found from its pane (FindPane).
func (r *Reader) Find(int) (string, string, error) {
	return "", "", &chat.Miss{Reason: chat.NoMatch}
}

func (r *Reader) db() string {
	if r.DB != "" {
		return r.DB
	}
	d := os.Getenv("XDG_DATA_HOME")
	if d == "" {
		h, _ := os.UserHomeDir()
		d = filepath.Join(h, ".local", "share")
	}
	return filepath.Join(d, "opencode", "opencode.db")
}

// open opens the database read-only, only while OpenCode has it open: a reader of a WAL database makes its -wal and
// -shm files when they aren't there (OpenCode stopped, and SQLite removed them).
func open(p string) (*sql.DB, error) {
	for _, f := range []string{p, p + "-wal", p + "-shm"} {
		if _, err := os.Stat(f); err != nil {
			return nil, &chat.Miss{Reason: chat.NoMatch}
		}
	}
	return sql.Open("sqlite", "file:"+p+"?mode=ro&_pragma=busy_timeout(2000)&_pragma=query_only(1)")
}

func split(path string) (db, id string, err error) {
	i := strings.LastIndexByte(path, '#')
	if i < 0 {
		return "", "", chat.ErrNoStep
	}
	return path[:i], path[i+1:], nil
}

// title is the terminal title OpenCode writes while it shows a session with this title.
func title(t string) string {
	if t == "" || fallback.MatchString(t) {
		return "OpenCode"
	}
	u := utf16.Encode([]rune(t))
	if len(u) <= 40 {
		return "OC | " + t
	}
	return "OC | " + string(utf16.Decode(u[:37])) + "…"
}

// fallback is the title OpenCode gives a session until it names it.
var fallback = regexp.MustCompile(`^(New|Child) session - \d{4}-\d{2}-\d{2}T\d{2}:\d{2}:\d{2}\.\d{3}Z$`)

// FindPane: the session OpenCode reported; else the one its terminal title names among its folder's; else none
// until it reports one. OpenCode 1 keeps its sessions in `session` and their messages in `message` and `part`
// (measured with 1.19.0, testdata/opencode/1.19.0), writes the same title, and reports no session: the title finds it.
func (r *Reader) FindPane(p chat.Pane) (string, string, error) {
	if len(p.Sessions) > 1 {
		return "", "", &chat.Miss{Reason: chat.Ambiguous}
	}
	path := r.db()
	d, err := open(path)
	if err != nil {
		if len(p.Sessions) == 0 {
			return "", "", &chat.Miss{Reason: chat.NotReported}
		}
		return "", "", err
	}
	defer d.Close()
	sessions := "session_v2"
	if !table(d, sessions) {
		if !table(d, "session") {
			return "", "", &chat.Miss{Reason: chat.NotReported}
		}
		sessions = "session" // OpenCode 1
	}
	if len(p.Sessions) == 1 {
		var v string
		err := d.QueryRow("SELECT version FROM "+sessions+" WHERE id = ?", p.Sessions[0]).Scan(&v)
		if errors.Is(err, sql.ErrNoRows) {
			return "", "", &chat.Miss{Reason: chat.NoMatch}
		}
		if err != nil {
			return "", "", chat.Formatf("%s: %v", sessions, err)
		}
		return path + "#" + p.Sessions[0], v, nil
	}
	if p.Title == "" || p.Title == "OpenCode" || !strings.HasPrefix(p.Title, "OC | ") {
		return "", "", &chat.Miss{Reason: chat.NotReported}
	}
	rows, err := d.Query("SELECT id, title, version FROM "+sessions+" WHERE directory = ? AND parent_id IS NULL AND time_archived IS NULL", p.Cwd)
	if err != nil {
		return "", "", chat.Formatf("%s: %v", sessions, err)
	}
	defer rows.Close()
	var id, v string
	n := 0
	for rows.Next() {
		var i, ver string
		var t sql.NullString
		if rows.Scan(&i, &t, &ver) == nil && title(t.String) == p.Title {
			id, v = i, ver
			n++
		}
	}
	if n != 1 {
		return "", "", &chat.Miss{Reason: chat.NotReported}
	}
	return path + "#" + id, v, nil
}

// table: the database has a table of this name.
func table(d *sql.DB, name string) bool {
	var n int
	return d.QueryRow("SELECT count(*) FROM sqlite_master WHERE type = 'table' AND name = ?", name).Scan(&n) == nil && n > 0
}

// rows reads a session's messages in order.
func rows(path string) ([]message, error) {
	p, id, err := split(path)
	if err != nil {
		return nil, err
	}
	d, err := open(p)
	if err != nil {
		return nil, err
	}
	defer d.Close()
	if !table(d, "session_v2") {
		return rows1(d, id)
	}
	rs, err := d.Query("SELECT id, type, seq, time_created, data FROM session_message WHERE session_id = ? ORDER BY seq", id)
	if err != nil {
		return nil, chat.Formatf("session_message: %v", err)
	}
	defer rs.Close()
	var ms []message
	for rs.Next() {
		var m message
		var data string
		if err := rs.Scan(&m.ID, &m.Type, &m.Seq, &m.Created, &data); err != nil {
			return nil, chat.Formatf("session_message: %v", err)
		}
		m.Data = json.RawMessage(data)
		ms = append(ms, m)
	}
	return ms, rs.Err()
}

// stamp changes when a session's messages do.
func stamp(path string) (string, error) {
	p, id, err := split(path)
	if err != nil {
		return "", err
	}
	d, err := open(p)
	if err != nil {
		return "", err
	}
	defer d.Close()
	var n, seq, upd int64
	if !table(d, "session_v2") {
		// OpenCode 1: its messages and their parts.
		if err := d.QueryRow(`SELECT (SELECT count(*) FROM message WHERE session_id = ?1) + (SELECT count(*) FROM part WHERE session_id = ?1),
			max(coalesce((SELECT max(time_updated) FROM message WHERE session_id = ?1), 0), coalesce((SELECT max(time_updated) FROM part WHERE session_id = ?1), 0))`, id).Scan(&n, &upd); err != nil {
			return "", chat.Formatf("message, part: %v", err)
		}
		return fmt.Sprint(n, upd), nil
	}
	if err := d.QueryRow("SELECT count(*), coalesce(max(seq), 0), coalesce(max(time_updated), 0) FROM session_message WHERE session_id = ?", id).Scan(&n, &seq, &upd); err != nil {
		return "", chat.Formatf("session_message: %v", err)
	}
	return fmt.Sprint(n, seq, upd), nil
}

// Turns reads the session's turns, again only when its messages changed.
func (r *Reader) Turns(path string) ([]chat.Turn, error) {
	st, err := stamp(path)
	if err != nil {
		return nil, err
	}
	r.mu.Lock()
	c, ok := r.cache[path]
	r.mu.Unlock()
	if ok && c.stamp == st {
		return c.turns, nil
	}
	ms, err := rows(path)
	if err != nil {
		return nil, err
	}
	ts := parse(ms)
	r.mu.Lock()
	if r.cache == nil {
		r.cache = map[string]cached{}
	}
	r.cache[path] = cached{stamp: st, turns: ts}
	r.mu.Unlock()
	return ts, nil
}

// find: the tool call or the text a step id names, in the session's rows.
func find(ms []message, step string) (*part, bool) {
	for _, m := range ms {
		if m.Type != "assistant" {
			continue
		}
		var d assistantData
		json.Unmarshal(m.Data, &d)
		for i, p := range d.Content {
			p := p
			if fmt.Sprintf("%s:%d", m.ID, i) == step && (p.Type == "text" || p.Type == "reasoning") {
				return &p, true
			}
			if p.Type != "tool" {
				continue
			}
			if p.ID == step || strings.HasPrefix(step, p.ID+":") || strings.HasPrefix(step, p.ID+"#") {
				return &p, true
			}
		}
	}
	return nil, false
}

// output: the text of a tool's result.
func output(p *part) string {
	var b []string
	for _, c := range p.State.Content {
		if c.Type == "text" {
			b = append(b, c.Text)
		}
	}
	return strings.Join(b, "\n")
}

var exited = regexp.MustCompile(`\n*Exited with code (\d+)\s*$`)

func lineCount(s string) int {
	if s == "" {
		return 0
	}
	return strings.Count(strings.TrimSuffix(s, "\n"), "\n") + 1
}

func (r *Reader) Detail(path, step string) (chat.Detail, error) {
	ms, err := rows(path)
	if err != nil {
		return chat.Detail{}, err
	}
	p, ok := find(ms, step)
	if !ok {
		return chat.Detail{}, chat.ErrNoStep
	}
	if p.Type != "tool" {
		return chat.Detail{Output: chat.Cut(p.Text, chat.MaxDetail)}, nil
	}
	out := output(p)
	in, _ := json.Marshal(p.State.Input)
	switch p.Name {
	case "shell", "bash":
		cmd := str(p.State.Input, "command")
		run := &chat.Run{Command: cmd, Code: exitCode(*p)}
		if m := exited.FindStringSubmatch(out); m != nil {
			run.Code, _ = strconv.Atoi(m[1])
			out = out[:len(out)-len(m[0])]
		}
		run.Failed = p.State.Status == "error" || run.Code != 0
		out = strings.TrimRight(out, "\n")
		run.Output, run.Lines = chat.CutEnd(out, chat.MaxDetail), lineCount(out)
		return chat.Detail{Input: chat.Cut(cmd, chat.MaxDetail), Output: run.Output, Run: run}, nil
	case "patch", "apply_patch", "edit", "write":
		var meta struct {
			Files []struct {
				File      string `json:"file"`
				Patch     string `json:"patch"`
				Additions int    `json:"additions"`
				Deletions int    `json:"deletions"`
			} `json:"files"`
		}
		json.Unmarshal(p.State.Metadata, &meta)
		i := 0
		if _, n, ok := strings.Cut(step, ":"); ok {
			i, _ = strconv.Atoi(n)
		}
		if i < len(meta.Files) {
			f := meta.Files[i]
			c := &chat.Change{File: f.File, Added: f.Additions, Deleted: f.Deletions}
			c.Hunks, c.Cut = chat.DiffHunks(f.Patch)
			return chat.Detail{Input: f.File, Output: chat.Cut(out, chat.MaxDetail), Change: c}, nil
		}
	}
	d := chat.Detail{Input: chat.Cut(string(in), chat.MaxDetail), Output: chat.Cut(out, chat.MaxDetail)}
	if p.Name == "read" {
		d.Input = str(p.State.Input, "path", "filePath")
	}
	for i, c := range p.State.Content {
		if c.Type == "file" && chat.ImageTypes[c.Mime] {
			d.Images = append(d.Images, chat.Image{ID: fmt.Sprintf("%s#%d", p.ID, i), Source: d.Input})
		}
	}
	return d, nil
}

// Image finds an image in a tool's result again (call id#n).
func (r *Reader) Image(path, id string) (string, []byte, error) {
	ms, err := rows(path)
	if err != nil {
		return "", nil, err
	}
	p, ok := find(ms, id)
	call, n, cut := strings.Cut(id, "#")
	if !ok || !cut || p.ID != call {
		return "", nil, chat.ErrNoStep
	}
	i, err := strconv.Atoi(n)
	if err != nil || i < 0 || i >= len(p.State.Content) {
		return "", nil, chat.ErrNoStep
	}
	c := p.State.Content[i]
	data, ok := strings.CutPrefix(c.URI, "data:"+c.Mime+";base64,")
	if c.Type != "file" || !chat.ImageTypes[c.Mime] || !ok {
		return "", nil, chat.ErrNoStep
	}
	b, err := base64.StdEncoding.DecodeString(data)
	if err != nil {
		return "", nil, chat.ErrNoStep
	}
	return c.Mime, b, nil
}

// rows1 reads an OpenCode 1 session's messages and their parts, as OpenCode 2's rows.
func rows1(d *sql.DB, id string) ([]message, error) {
	rs, err := d.Query("SELECT id, time_created, data FROM message WHERE session_id = ? ORDER BY time_created, id", id)
	if err != nil {
		return nil, chat.Formatf("message: %v", err)
	}
	defer rs.Close()
	var ms []msg1
	for rs.Next() {
		var m msg1
		var data string
		if err := rs.Scan(&m.ID, &m.Created, &data); err != nil {
			return nil, chat.Formatf("message: %v", err)
		}
		m.Data = json.RawMessage(data)
		ms = append(ms, m)
	}
	if err := rs.Err(); err != nil {
		return nil, err
	}
	ps, err := d.Query("SELECT id, message_id, data FROM part WHERE session_id = ? ORDER BY id", id)
	if err != nil {
		return nil, chat.Formatf("part: %v", err)
	}
	defer ps.Close()
	var parts []part1
	for ps.Next() {
		var p part1
		var data string
		if err := ps.Scan(&p.ID, &p.Message, &data); err != nil {
			return nil, chat.Formatf("part: %v", err)
		}
		p.Data = json.RawMessage(data)
		parts = append(parts, p)
	}
	return convert1(ms, parts), ps.Err()
}

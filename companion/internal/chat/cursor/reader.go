// Package cursor reads Cursor CLI's active prompt-message graph (2026.11.19-f005e9d).
package cursor

import (
	"crypto/md5"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	_ "modernc.org/sqlite"

	"herdr-anywhere/companion/internal/chat"
)

const LabelCwd = "herdr-anywhere.chat.cursor.cwd"

var conversationID = regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`)
var blobID = regexp.MustCompile(`^[0-9a-f]{64}$`)

func ConfigRoot() string {
	if d := os.Getenv("CURSOR_CONFIG_DIR"); strings.TrimSpace(d) != "" {
		return d
	}
	if d := os.Getenv("XDG_CONFIG_HOME"); strings.TrimSpace(d) != "" {
		return filepath.Join(d, "cursor")
	}
	h, _ := os.UserHomeDir()
	return filepath.Join(h, ".cursor")
}

type Reader struct {
	Dir     string
	Setup   *Setup
	started func(int) time.Time
	now     func() time.Time
}

func (r *Reader) ProgramName() string { return "cursor-agent" }

func (r *Reader) root() string {
	if r.Dir != "" {
		return r.Dir
	}
	return ConfigRoot()
}
func (r *Reader) Find(int) (string, string, error) { return "", "", &chat.Miss{Reason: chat.NoMatch} }

func (r *Reader) FindPane(p chat.Pane) (string, string, error) {
	if !supported() {
		return "", "", &chat.Miss{Reason: chat.NoMatch}
	}
	if r.Setup != nil && r.Setup.State() != chat.Installed {
		return "", "", &chat.Miss{Reason: chat.SetupNeeded}
	}
	id, ok := p.Labeled()
	if !ok || p.Labels[LabelCwd] == "" {
		reason := chat.SetupNotLoaded
		if r.Setup != nil && r.Setup.State() != chat.Installed {
			reason = chat.SetupNeeded
		} else if r.Setup != nil {
			started := r.started
			if started == nil {
				started = processStarted
			}
			if info, err := os.Stat(r.Setup.File()); err == nil && started(p.Pid).After(info.ModTime()) {
				reason = chat.AwaitingPrompt
			}
		}
		return "", "", &chat.Miss{Reason: reason}
	}
	cwd := p.Labels[LabelCwd]
	if !conversationID.MatchString(id) || !filepath.IsAbs(cwd) || filepath.Clean(cwd) != cwd {
		return "", "", &chat.Miss{Reason: chat.NoMatch}
	}
	path := filepath.Join(r.root(), "chats", fmt.Sprintf("%x", md5.Sum([]byte(cwd))), id, "store.db")
	if !r.valid(path) {
		return "", "", &chat.Miss{Reason: chat.NoMatch}
	}
	if _, err := os.Stat(path); err != nil {
		return "", "", &chat.Miss{Reason: chat.NoRecordYet}
	}
	return path, "", nil
}

func (r *Reader) valid(path string) bool {
	root, err := filepath.Abs(filepath.Join(r.root(), "chats"))
	if err != nil {
		return false
	}
	p, err := filepath.Abs(path)
	if err != nil || filepath.Clean(path) != path {
		return false
	}
	rel, err := filepath.Rel(root, p)
	if err != nil {
		return false
	}
	bits := strings.Split(rel, string(filepath.Separator))
	if len(bits) != 3 || len(bits[0]) != 32 || !conversationID.MatchString(bits[1]) || bits[2] != "store.db" {
		return false
	}
	if _, err := hex.DecodeString(bits[0]); err != nil {
		return false
	}
	// Resolve both sides so a link inside chats cannot escape the configured root.
	realRoot, e1 := filepath.EvalSymlinks(root)
	realPath, e2 := filepath.EvalSymlinks(p)
	if e1 == nil && e2 == nil {
		return realPath == filepath.Join(realRoot, rel)
	}
	return os.IsNotExist(e2)
}

type part struct {
	Mime  string `json:"mimeType"`
	Image struct {
		Type string `json:"__type"`
		Hex  string `json:"hex"`
	} `json:"image"`
	Type         string          `json:"type"`
	Text         string          `json:"text"`
	ToolCallID   string          `json:"toolCallId"`
	ToolName     string          `json:"toolName"`
	Args         json.RawMessage `json:"args"`
	Result       json.RawMessage `json:"result"`
	Experimental []struct {
		Type string `json:"type"`
		Data string `json:"data"`
		Mime string `json:"mimeType"`
	} `json:"experimental_content"`
}
type message struct {
	id       string
	turnID   string
	Role     string          `json:"role"`
	Content  json.RawMessage `json:"content"`
	Provider struct {
		Cursor struct {
			High toolResult `json:"highLevelToolCallResult"`
		} `json:"cursor"`
	} `json:"providerOptions"`
}
type toolResult struct {
	IsError bool `json:"isError"`
	Output  struct {
		Rejected json.RawMessage `json:"rejected"`
		Failure  *struct {
			ExitCode    int    `json:"exitCode"`
			Interleaved string `json:"interleavedOutput"`
		} `json:"failure"`
		Success struct {
			ExitCode int    `json:"exitCode"`
			Stdout   string `json:"stdout"`
			Diff     string `json:"diffString"`
			Path     string `json:"path"`
			Added    int    `json:"linesAdded"`
			Deleted  int    `json:"linesRemoved"`
		} `json:"success"`
	} `json:"output"`
}

// promptRefs skips unknown protobuf fields; only field 1 names active ordered messages.
func promptRefs(b []byte) ([]string, error) {
	var refs []string
	for len(b) > 0 {
		tag, n := binary.Uvarint(b)
		if n == 0 {
			return nil, io.ErrUnexpectedEOF
		}
		if n <= 0 || tag>>3 == 0 {
			return nil, fmt.Errorf("invalid protobuf tag")
		}
		b = b[n:]
		switch tag & 7 {
		case 0:
			_, n = binary.Uvarint(b)
			if n == 0 {
				return nil, io.ErrUnexpectedEOF
			}
			if n <= 0 {
				return nil, fmt.Errorf("invalid protobuf varint")
			}
			b = b[n:]
		case 1, 5:
			size := 8
			if tag&7 == 5 {
				size = 4
			}
			if len(b) < size {
				return nil, io.ErrUnexpectedEOF
			}
			b = b[size:]
		case 2:
			size, n := binary.Uvarint(b)
			if n == 0 || n > 0 && size > uint64(len(b)-n) {
				return nil, io.ErrUnexpectedEOF
			}
			if n < 0 {
				return nil, fmt.Errorf("invalid protobuf length")
			}
			b = b[n:]
			if tag>>3 == 1 {
				if size != 32 {
					return nil, fmt.Errorf("invalid prompt reference")
				}
				refs = append(refs, hex.EncodeToString(b[:size]))
			}
			b = b[size:]
		default:
			return nil, fmt.Errorf("unsupported protobuf wire type")
		}
	}
	return refs, nil
}

func (r *Reader) messages(path string) ([]message, error) {
	ms, _, err := r.pageMessages(path, "", "")
	return ms, err
}

func (r *Reader) readError(path, field string, err error) error {
	if errors.Is(err, sql.ErrNoRows) || errors.Is(err, io.ErrUnexpectedEOF) || errors.Is(err, io.EOF) {
		now := time.Now
		if r.now != nil {
			now = r.now
		}
		for _, file := range []string{path, path + "-wal"} {
			if info, e := os.Stat(file); e == nil {
				age := now().Sub(info.ModTime())
				if age >= 0 && age < 10*time.Second {
					return &chat.Miss{Reason: chat.NoRecordYet}
				}
			}
		}
	}
	return chat.Formatf("Cursor %s: %v", field, err)
}

func (r *Reader) jsonError(path, field string, data []byte) error {
	var value json.RawMessage
	err := json.NewDecoder(strings.NewReader(string(data))).Decode(&value)
	if err == nil {
		err = fmt.Errorf("invalid JSON")
	}
	return r.readError(path, field, err)
}

func (r *Reader) pageMessages(path, before, target string) ([]message, string, error) {
	if !r.valid(path) {
		return nil, "", &chat.Miss{Reason: chat.NoMatch}
	}
	// Like OpenCode, do not create WAL sidecars in an agent's stopped database.
	for _, f := range []string{path, path + "-wal", path + "-shm"} {
		if _, err := os.Stat(f); err != nil {
			return nil, "", &chat.Miss{Reason: chat.NoRecordYet}
		}
	}
	uri := chat.SQLiteFileURI(filepath.ToSlash(path))
	d, err := sql.Open("sqlite", uri+"?mode=ro&_pragma=busy_timeout(2000)&_pragma=query_only(1)")
	if err != nil {
		return nil, "", err
	}
	defer d.Close()
	tx, err := d.Begin()
	if err != nil {
		return nil, "", err
	}
	defer tx.Rollback()
	var raw string
	if err = tx.QueryRow(`SELECT value FROM meta WHERE key='0'`).Scan(&raw); err != nil {
		return nil, "", r.readError(path, "metadata", err)
	}
	b, err := hex.DecodeString(raw)
	if err != nil {
		return nil, "", chat.Formatf("Cursor metadata encoding")
	}
	var meta struct {
		AgentID string  `json:"agentId"`
		Root    *string `json:"latestRootBlobId"`
	}
	if !json.Valid(b) {
		return nil, "", r.jsonError(path, "metadata JSON", b)
	}
	if json.Unmarshal(b, &meta) != nil || meta.Root == nil || meta.AgentID != filepath.Base(filepath.Dir(path)) {
		return nil, "", chat.Formatf("Cursor metadata identity")
	}
	// AgentStore's initial latestRootBlobId is an empty Uint8Array (4945.index.js, 2026.11.19-f005e9d).
	if *meta.Root == "" {
		return nil, "", &chat.Miss{Reason: chat.NoRecordYet}
	}
	if !blobID.MatchString(*meta.Root) {
		return nil, "", chat.Formatf("Cursor metadata root id")
	}
	var root []byte
	if err = tx.QueryRow(`SELECT data FROM blobs WHERE id=?`, *meta.Root).Scan(&root); err != nil {
		return nil, "", r.readError(path, "root", err)
	}
	refs, err := promptRefs(root)
	if err != nil {
		return nil, "", r.readError(path, "root", err)
	}
	type boundary struct {
		id    string
		start int
	}
	var turns []boundary
	identity := sha256.New()
	found := -1
	for i, id := range refs {
		var role, content sql.NullString
		var valid bool
		if err := tx.QueryRow(`SELECT json_valid(data), CASE WHEN json_valid(data) THEN json_extract(data,'$.role') END, CASE WHEN json_valid(data) THEN json_type(data,'$.content') END FROM blobs WHERE id=?`, id).Scan(&valid, &role, &content); err != nil {
			return nil, "", r.readError(path, "message index", err)
		}
		if !valid {
			var data []byte
			if err := tx.QueryRow(`SELECT substr(data,1,?) FROM blobs WHERE id=?`, chat.MaxBack, id).Scan(&data); err != nil {
				return nil, "", r.readError(path, "message JSON", err)
			}
			return nil, "", r.jsonError(path, "message JSON", data)
		}
		if role.String == "user" && (content.String == "array" || content.String == "null") {
			identity.Write([]byte(id))
			turnID := "m" + hex.EncodeToString(identity.Sum(nil))
			turns = append(turns, boundary{turnID, i})
			if target != "" && strings.HasPrefix(target, turnID+"/image/") {
				found = len(turns) - 1
			}
		}
		if target == "" || len(turns) == 0 || role.String != "assistant" || content.String != "array" {
			continue
		}
		rows, err := tx.Query(`SELECT json_extract(p.value,'$.toolCallId') FROM blobs AS b, json_each(b.data,'$.content') AS p WHERE b.id=? AND json_extract(p.value,'$.type')='tool-call'`, id)
		if err != nil {
			return nil, "", chat.Formatf("Cursor tool index: %v", err)
		}
		for rows.Next() {
			var call sql.NullString
			if err := rows.Scan(&call); err != nil {
				rows.Close()
				return nil, "", err
			}
			if call.String != "" && (target == call.String || target == call.String+"-note" || strings.HasPrefix(target, call.String+"/image/")) {
				found = len(turns) - 1
			}
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, "", err
		}
	}
	end := len(turns)
	if before != "" {
		end = -1
		for i, t := range turns {
			if t.id == before {
				end = i
				break
			}
		}
		if end < 0 {
			return nil, "", chat.ErrCursor
		}
	}
	start := max(0, end-chat.MaxTurns)
	if target != "" {
		if found < 0 {
			return nil, "", chat.ErrNoStep
		}
		start, end = found, found+1
	}
	next := ""
	if start > 0 {
		next = turns[start].id
	}
	if start == end {
		return nil, next, nil
	}
	last := len(refs)
	if end < len(turns) {
		last = turns[end].start
	}
	var messages []message
	budget := chat.MaxBack
	turn := start
	for i := turns[start].start; i < last; i++ {
		id := refs[i]
		var data []byte
		var size int
		if err = tx.QueryRow(`SELECT length(data), CASE WHEN length(data)<=? THEN data ELSE NULL END FROM blobs WHERE id=?`, budget, id).Scan(&size, &data); err != nil {
			return nil, "", r.readError(path, "message", err)
		}
		budget -= size
		if budget < 0 {
			return nil, "", chat.ErrHistoryLimit
		}
		var m message
		if json.Unmarshal(data, &m) != nil {
			return nil, "", chat.Formatf("Cursor message JSON")
		}
		if turn+1 < end && i >= turns[turn+1].start {
			turn++
		}
		m.turnID = turns[turn].id
		messages = append(messages, m)
		messages[len(messages)-1].id = id
	}
	return messages, next, nil
}

type parsed struct {
	turns   []chat.Turn
	details map[string]chat.Detail
	images  map[string]struct {
		media string
		data  []byte
	}
}

var query = regexp.MustCompile(`(?s)<user_query>\s*(.*?)\s*</user_query>`)
var timestamp = regexp.MustCompile(`<timestamp>(.*?)</timestamp>`)

func parseMessages(ms []message) (parsed, error) {
	p := parsed{turns: []chat.Turn{}, details: map[string]chat.Detail{}, images: map[string]struct {
		media string
		data  []byte
	}{}}
	type location struct {
		turn, step int
		args       map[string]any
	}
	calls := map[string]location{}
	var turn *chat.Turn
	for _, m := range ms {
		var parts []part
		if json.Unmarshal(m.Content, &parts) != nil {
			continue
		} // system and Cursor's environment context are strings
		if m.Role == "user" {
			var text []string
			for _, c := range parts {
				if c.Type == "text" {
					text = append(text, c.Text)
				}
			}
			said := strings.Join(text, "\n")
			if q := query.FindStringSubmatch(said); q != nil {
				said = q[1]
			}
			if turn != nil && turn.Reply != "" {
				turn.End = "done"
			}
			t := chat.Turn{ID: m.turnID, From: "you", Said: said, End: "open", Steps: []chat.Step{}}
			for i, c := range parts {
				if c.Type != "image" || c.Image.Type != "Uint8Array" || !chat.ImageTypes[c.Mime] {
					continue
				}
				data, err := hex.DecodeString(c.Image.Hex)
				if err != nil || len(data) == 0 {
					continue
				}
				id := t.ID + "/image/" + strconv.Itoa(i)
				p.images[id] = struct {
					media string
					data  []byte
				}{c.Mime, data}
				t.Images = append(t.Images, chat.Image{ID: id})
			}
			if at := timestamp.FindStringSubmatch(strings.Join(text, "\n")); at != nil {
				if at, err := time.Parse("Monday, Jan 2, 2006, 3:04 PM (MST)", at[1]); err == nil {
					t.At = at.Format(time.RFC3339)
				}
			}
			p.turns = append(p.turns, t)
			turn = &p.turns[len(p.turns)-1]
		}
		if turn == nil {
			continue
		}
		for _, c := range parts {
			switch {
			case m.Role == "assistant" && c.Type == "text":
				if turn.Reply != "" {
					turn.Reply += "\n\n"
				}
				turn.Reply += c.Text
				turn.End = "done"
			case m.Role == "assistant" && c.Type == "tool-call":
				args := map[string]any{}
				_ = json.Unmarshal(c.Args, &args)
				kind := "other"
				title := c.ToolName
				switch c.ToolName {
				case "Shell":
					kind = "run"
				case "Read":
					kind = "read"
				case "StrReplace", "Write":
					kind = "edit"
				case "Grep", "Glob":
					kind = "search"
				case "WebFetch", "WebSearch":
					kind = "web"
				}
				for _, key := range []string{"command", "path", "file_path", "pattern"} {
					if v, ok := args[key].(string); ok && v != "" {
						title = v
						break
					}
				}
				id := c.ToolCallID
				if id == "" {
					return p, chat.Formatf("Cursor tool call without id")
				}
				if turn.Reply != "" {
					turn.Steps = append(turn.Steps, chat.Step{ID: id + "-note", Kind: "note", Title: turn.Reply})
					p.details[id+"-note"] = chat.Detail{Input: "", Output: chat.Cut(turn.Reply, chat.MaxDetail)}
					turn.Reply = ""
				}
				calls[id] = location{len(p.turns) - 1, len(turn.Steps), args}
				turn.Steps = append(turn.Steps, chat.Step{ID: id, Kind: kind, Title: title})
				turn.End = "open"
				p.details[id] = chat.Detail{Input: chat.Cut(string(c.Args), chat.MaxDetail), Output: ""}
			case m.Role == "tool" && c.Type == "tool-result":
				loc, ok := calls[c.ToolCallID]
				if !ok {
					continue
				}
				step := &p.turns[loc.turn].Steps[loc.step]
				h := m.Provider.Cursor.High
				step.Failed = h.IsError || len(h.Output.Rejected) > 0 && string(h.Output.Rejected) != "null" || h.Output.Failure != nil || h.Output.Success.ExitCode != 0
				d := p.details[c.ToolCallID]
				var output string
				if json.Unmarshal(c.Result, &output) != nil {
					output = string(c.Result)
				}
				d.Output = chat.Cut(output, chat.MaxDetail)
				if step.Kind == "run" {
					code := h.Output.Success.ExitCode
					if h.Output.Failure != nil {
						code = h.Output.Failure.ExitCode
					}
					step.Failed = step.Failed || code != 0
					stdout := h.Output.Success.Stdout
					if h.Output.Failure != nil {
						stdout = h.Output.Failure.Interleaved
					}
					d.Run = &chat.Run{Command: step.Title, Failed: step.Failed, Code: code, Output: chat.Cut(stdout, chat.MaxDetail), Lines: strings.Count(strings.TrimSuffix(stdout, "\n"), "\n") + 1}
				}
				if s := h.Output.Success; s.Diff != "" {
					hunks, cut := chat.DiffHunks(s.Diff)
					d.Change = &chat.Change{File: s.Path, Added: s.Added, Deleted: s.Deleted, Hunks: hunks, Cut: cut}
					step.Added = s.Added
					step.Deleted = s.Deleted
				}
				for i, im := range c.Experimental {
					if im.Type != "image" || !chat.ImageTypes[im.Mime] {
						continue
					}
					data, e := base64.StdEncoding.DecodeString(im.Data)
					if e != nil {
						continue
					}
					id := c.ToolCallID + "/image/" + strconv.Itoa(i)
					p.images[id] = struct {
						media string
						data  []byte
					}{im.Mime, data}
					d.Images = append(d.Images, chat.Image{ID: id, Source: step.Title})
				}
				p.details[c.ToolCallID] = d
			}
		}
	}
	return p, nil
}

func (r *Reader) Turns(path string) ([]chat.Turn, error) {
	p, _, e := r.parse(path, "", "")
	return p.turns, e
}

func (r *Reader) parse(path, before, target string) (parsed, string, error) {
	ms, next, err := r.pageMessages(path, before, target)
	if err != nil {
		return parsed{}, "", err
	}
	p, err := parseMessages(ms)
	return p, next, err
}

func (r *Reader) Before(path, first string) (string, error) { return first, nil }

func (r *Reader) History(path, before string) (chat.HistoryPage, error) {
	p, next, err := r.parse(path, before, "")
	if err != nil {
		return chat.HistoryPage{}, err
	}
	return chat.HistoryPage{Turns: p.turns, Before: next}, nil
}
func (r *Reader) Detail(path, id string) (chat.Detail, error) {
	p, _, e := r.parse(path, "", id)
	if e != nil {
		return chat.Detail{}, e
	}
	if d, ok := p.details[id]; ok {
		return d, nil
	}
	return chat.Detail{}, chat.ErrNoStep
}
func (r *Reader) Image(path, id string) (string, []byte, error) {
	p, _, e := r.parse(path, "", id)
	if e != nil {
		return "", nil, e
	}
	if im, ok := p.images[id]; ok {
		return im.media, im.data, nil
	}
	return "", nil, chat.ErrNoStep
}

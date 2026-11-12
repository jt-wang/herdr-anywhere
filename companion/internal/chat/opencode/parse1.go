package opencode

// OpenCode 1, measured with 1.19.0 against the fake Responses API (testdata/opencode/1.19.0): a session's messages are
// rows of `message` (data: role, time.created and time.completed, finish, error, path.cwd) and what each says rows of
// `part` (data.type: text, reasoning, tool, step-start, step-finish). The model's every call is an assistant message
// of its own; the turn's last ends with a finish other than "tool-calls". A tool part holds callID, tool and state
// (status, input, output, metadata, attachments). Here they become the rows OpenCode 2 writes (session_message), so
// one parse reads both.

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"strings"
)

// msg1 is a row of OpenCode 1's message table; part1 of its part table.
type msg1 struct {
	ID      string
	Created int64
	Data    json.RawMessage
}

type part1 struct {
	ID, Message string
	Data        json.RawMessage
}

type msgData1 struct {
	Role string `json:"role"`
	Time struct {
		Created   int64 `json:"created"`
		Completed int64 `json:"completed"`
	} `json:"time"`
	Finish string          `json:"finish"`
	Error  json.RawMessage `json:"error"`
	Path   struct {
		Cwd string `json:"cwd"`
	} `json:"path"`
}

type partData1 struct {
	Type      string `json:"type"`
	Text      string `json:"text"`
	Synthetic bool   `json:"synthetic"`
	Ignored   bool   `json:"ignored"`
	Tool      string `json:"tool"`
	CallID    string `json:"callID"`
	State     struct {
		Status      string                     `json:"status"`
		Input       map[string]json.RawMessage `json:"input"`
		Output      string                     `json:"output"`
		Error       string                     `json:"error"`
		Metadata    json.RawMessage            `json:"metadata"`
		Attachments []struct {
			Type string `json:"type"`
			Mime string `json:"mime"`
			URL  string `json:"url"`
		} `json:"attachments"`
	} `json:"state"`
}

// convert1 makes OpenCode 1's rows into OpenCode 2's: a user message with its text, an assistant message with its
// parts as content, and after a turn's last assistant message the end row ("idle") it would have written.
func convert1(ms []msg1, ps []part1) []message {
	parts := map[string][]part1{}
	for _, p := range ps {
		parts[p.Message] = append(parts[p.Message], p)
	}
	for _, l := range parts {
		sort.Slice(l, func(i, j int) bool { return l[i].ID < l[j].ID }) // ids grow with time
	}
	ms = append([]msg1(nil), ms...)
	sort.SliceStable(ms, func(i, j int) bool {
		if ms[i].Created != ms[j].Created {
			return ms[i].Created < ms[j].Created
		}
		return ms[i].ID < ms[j].ID
	})
	var out []message
	for _, m := range ms {
		var d msgData1
		json.Unmarshal(m.Data, &d)
		switch d.Role {
		case "user":
			var said []string
			for _, p := range parts[m.ID] {
				var pd partData1
				json.Unmarshal(p.Data, &pd)
				if pd.Type == "text" && !pd.Synthetic && !pd.Ignored {
					said = append(said, pd.Text)
				}
			}
			b, _ := json.Marshal(userData{Text: strings.Join(said, "\n")})
			out = append(out, message{ID: m.ID, Type: "user", Created: m.Created, Data: b})
		case "assistant":
			var content []json.RawMessage
			for _, p := range parts[m.ID] {
				if c := content1(p, d.Path.Cwd); c != nil {
					content = append(content, c)
				}
			}
			b, _ := json.Marshal(map[string]any{"content": content})
			out = append(out, message{ID: m.ID, Type: "assistant", Created: m.Created, Data: b})
			failed := len(d.Error) > 0 && string(d.Error) != "null"
			if failed || (d.Time.Completed > 0 && d.Finish != "" && d.Finish != "tool-calls") {
				outcome := "succeeded"
				if failed {
					outcome = "failed"
				}
				b, _ := json.Marshal(idleData{Outcome: outcome})
				out = append(out, message{ID: m.ID + "#end", Type: "idle", Created: d.Time.Completed, Data: b})
			}
		}
	}
	for i := range out {
		out[i].Seq = int64(i)
	}
	return out
}

// content1: one of OpenCode 1's parts as a content item of OpenCode 2's (nil for the step-start/finish markers).
func content1(p part1, cwd string) json.RawMessage {
	var d partData1
	json.Unmarshal(p.Data, &d)
	var v any
	switch d.Type {
	case "text", "reasoning":
		if d.Synthetic {
			return nil
		}
		v = map[string]any{"type": d.Type, "text": d.Text}
	case "tool":
		type item struct {
			Type string `json:"type"`
			Text string `json:"text,omitempty"`
			URI  string `json:"uri,omitempty"`
			Mime string `json:"mime,omitempty"`
		}
		items := []item{}
		if d.State.Output != "" {
			items = append(items, item{Type: "text", Text: d.State.Output})
		}
		if d.State.Error != "" {
			items = append(items, item{Type: "text", Text: d.State.Error})
		}
		for _, a := range d.State.Attachments {
			items = append(items, item{Type: a.Type, URI: a.URL, Mime: a.Mime})
		}
		v = map[string]any{"type": "tool", "id": d.CallID, "name": d.Tool, "state": map[string]any{
			"status": d.State.Status, "input": d.State.Input, "metadata": metadata1(d.State.Metadata, cwd), "content": items}}
	default:
		return nil
	}
	b, _ := json.Marshal(v)
	return b
}

// metadata1: a tool result's metadata with apply_patch's files named as OpenCode 2 names them (`file`, from the
// session's folder).
func metadata1(raw json.RawMessage, cwd string) json.RawMessage {
	var m map[string]json.RawMessage
	if json.Unmarshal(raw, &m) != nil || m["files"] == nil {
		return raw
	}
	var files []map[string]any
	if json.Unmarshal(m["files"], &files) != nil {
		return raw
	}
	for _, f := range files {
		if _, ok := f["file"]; ok {
			continue
		}
		name, _ := f["filePath"].(string)
		if rel, err := filepath.Rel(cwd, name); cwd != "" && err == nil && !strings.HasPrefix(rel, "..") {
			name = rel
		}
		f["file"] = filepath.ToSlash(name)
	}
	b, _ := json.Marshal(files)
	m["files"] = b
	out, _ := json.Marshal(m)
	return out
}

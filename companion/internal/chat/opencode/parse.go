// Package opencode reads OpenCode's sessions for the chat view: the rows of one session in OpenCode's own database
// (OpenCode 2's session_message; OpenCode 1's message and part, made into the same rows), made into turns and steps.
// Which session a pane shows is the id OpenCode 2 reports to herdr-core in OSC 7501 (`hc get`'s records); OpenCode 1
// reports none, and its terminal title names it.
//
// The way sessions are bound to panes learned from Heeler (https://github.com/ZingerLittleBee/Heeler, Apache-2.0);
// the code is written here.
package opencode

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"herdr-anywhere/companion/internal/chat"
)

// message is one row of session_message: a person's prompt ("user"), what the model answered and did
// ("assistant"), or the end of a turn ("idle").
type message struct {
	ID      string          `json:"id"`
	Type    string          `json:"type"`
	Seq     int64           `json:"seq"`
	Created int64           `json:"time_created"`
	Data    json.RawMessage `json:"data"`
}

type userData struct {
	Text string `json:"text"`
}

type part struct {
	Type  string `json:"type"`
	ID    string `json:"id"`
	Name  string `json:"name"`
	Text  string `json:"text"`
	State struct {
		Status   string                     `json:"status"`
		Input    map[string]json.RawMessage `json:"input"`
		Metadata json.RawMessage            `json:"metadata"`
		Content  []struct {
			Type string `json:"type"`
			Text string `json:"text"`
			URI  string `json:"uri"`
			Mime string `json:"mime"`
		} `json:"content"`
	} `json:"state"`
}

type assistantData struct {
	Content []part `json:"content"`
}

type idleData struct {
	Outcome string `json:"outcome"`
}

func str(m map[string]json.RawMessage, keys ...string) string {
	for _, k := range keys {
		var s string
		if json.Unmarshal(m[k], &s) == nil && s != "" {
			return s
		}
	}
	return ""
}

// firstLine: a text's first line that has something on it.
func firstLine(s string) string {
	for _, l := range strings.Split(s, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			return l
		}
	}
	return ""
}

// patchFiles: the files a patch tool changed, from its result's metadata, else from the patch's own headers.
func patchFiles(p part) []chat.Step {
	var meta struct {
		Files []struct {
			File      string `json:"file"`
			Additions int    `json:"additions"`
			Deletions int    `json:"deletions"`
		} `json:"files"`
	}
	var steps []chat.Step
	if json.Unmarshal(p.State.Metadata, &meta) == nil {
		for _, f := range meta.Files {
			steps = append(steps, chat.Step{Kind: "edit", Title: f.File, Added: f.Additions, Deleted: f.Deletions})
		}
	}
	if len(steps) == 0 {
		for _, l := range strings.Split(str(p.State.Input, "patchText"), "\n") {
			for _, h := range []string{"*** Add File: ", "*** Update File: ", "*** Delete File: "} {
				if name, ok := strings.CutPrefix(l, h); ok {
					steps = append(steps, chat.Step{Kind: "edit", Title: strings.TrimSpace(name)})
				}
			}
		}
	}
	return steps
}

// exitCode: a command's exit code from its result's metadata; 0 when it says none.
func exitCode(p part) int {
	var meta struct {
		Exit int `json:"exit"`
	}
	json.Unmarshal(p.State.Metadata, &meta)
	return meta.Exit
}

// toolSteps: the steps one tool call shows as.
func toolSteps(p part) []chat.Step {
	failed := p.State.Status == "error"
	switch p.Name {
	case "shell", "bash":
		return []chat.Step{{ID: p.ID, Kind: "run", Title: str(p.State.Input, "command"), Failed: failed || exitCode(p) != 0}}
	case "patch", "apply_patch", "edit", "write":
		steps := patchFiles(p)
		if len(steps) == 0 {
			steps = []chat.Step{{Kind: "edit", Title: str(p.State.Input, "path", "filePath")}}
		}
		for i := range steps {
			steps[i].ID = fmt.Sprintf("%s:%d", p.ID, i)
			steps[i].Failed = failed
		}
		return steps
	case "read":
		return []chat.Step{{ID: p.ID, Kind: "read", Title: str(p.State.Input, "path", "filePath"), Failed: failed}}
	case "glob", "grep":
		return []chat.Step{{ID: p.ID, Kind: "search", Title: str(p.State.Input, "pattern"), Failed: failed}}
	case "webfetch", "websearch":
		return []chat.Step{{ID: p.ID, Kind: "web", Title: str(p.State.Input, "url", "query"), Failed: failed}}
	case "subagent", "task":
		return []chat.Step{{ID: p.ID, Kind: "agent", Title: str(p.State.Input, "description", "prompt"), Failed: failed}}
	}
	return []chat.Step{{ID: p.ID, Kind: "other", Title: p.Name, Failed: failed}}
}

func at(ms int64) string {
	if ms == 0 {
		return ""
	}
	return time.UnixMilli(ms).UTC().Format(time.RFC3339Nano)
}

// parse makes turns of a session's rows, in their order: a prompt opens a turn, the model's texts before its last
// are notes between the steps, its last text is the reply, and the end row closes the turn.
func parse(msgs []message) []chat.Turn {
	var turns []chat.Turn
	var cur *chat.Turn
	var started int64
	pending, pendingID := "", ""
	flush := func() {
		if cur != nil && pending != "" {
			cur.Steps = append(cur.Steps, chat.Step{ID: pendingID, Kind: "note", Title: pending})
		}
		pending, pendingID = "", ""
	}
	finish := func(end string, ms int64) {
		if cur == nil {
			return
		}
		cur.Reply, pending = pending, ""
		cur.End = end
		if ms > started && started > 0 {
			cur.Secs = int((ms - started) / 1000)
		}
		turns = append(turns, *cur)
		cur = nil
	}
	for _, m := range msgs {
		switch m.Type {
		case "user":
			finish("stopped", 0)
			var d userData
			json.Unmarshal(m.Data, &d)
			cur = &chat.Turn{ID: m.ID, From: "you", Said: d.Text, At: at(m.Created), Steps: []chat.Step{}, End: "open"}
			started = m.Created
		case "assistant":
			if cur == nil {
				continue
			}
			var d assistantData
			json.Unmarshal(m.Data, &d)
			for i, p := range d.Content {
				switch p.Type {
				case "text":
					flush()
					pending, pendingID = p.Text, fmt.Sprintf("%s:%d", m.ID, i)
				case "reasoning":
					flush()
					if t := firstLine(p.Text); t != "" {
						cur.Steps = append(cur.Steps, chat.Step{ID: fmt.Sprintf("%s:%d", m.ID, i), Kind: "think", Title: t})
					}
				case "tool":
					flush()
					cur.Steps = append(cur.Steps, toolSteps(p)...)
				}
			}
		case "idle":
			var d idleData
			json.Unmarshal(m.Data, &d)
			end := "done"
			if d.Outcome != "succeeded" {
				end = "stopped"
			}
			finish(end, m.Created)
		}
	}
	if cur != nil {
		// Still working: what it said last is the latest note, not yet a reply.
		flush()
		turns = append(turns, *cur)
	}
	if len(turns) > chat.MaxTurns {
		turns = turns[len(turns)-chat.MaxTurns:]
	}
	return turns
}

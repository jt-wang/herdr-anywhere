// Package fakeapi is a stand-in for the Anthropic Messages API, for tests only: the installed Claude Code runs against
// it (ANTHROPIC_BASE_URL) in a throwaway CLAUDE_CONFIG_DIR, so its session records come from the real program with
// made-up words, no login and no tokens. The reader's fixtures and the live check are made this way.
//
// The answer depends on a word in the person's last prompt:
//
//	HELLO  one sentence, end_turn
//	LIST   a sentence, a Bash call (ls), then after its result a sentence, end_turn
//	EDIT   a Write call (hello.txt in the session's folder), then a sentence
//	ASK    an AskUserQuestion call; after the answer, a sentence
//	SLOW   a Bash call that takes 60 s (to interrupt); Claude Code 2.1.319 refuses a bare `sleep 60`
//	THINK  a thinking block with its text, then a sentence
//	LOOK   a Read call of shot.png in the session's folder (an image result), then a sentence
//	FIND   a Grep call, a Glob call, then a sentence
//	HELP   an Agent call whose prompt says HELLO (the sub-agent's requests come here too), then a sentence
//	WRITE  a reply in Markdown: a heading, a list, code, a table, a link
//	FAIL   an API error (400), as a refused request
//	CHANGE a Read of notes.txt, an Edit of it (one line becomes three), then a sentence
//	BREAK  a Bash call that prints two lines and exits 3, then a sentence
//	RENDER a sentence naming the files it made: a video and a PDF, by paths in the session's folder
//
// Anything else (side requests such as titles, prompts with none of these words) gets "OK.".
package fakeapi

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

type block = map[string]any

// Step is one answer of a script: content blocks and the stop reason.
type Step struct {
	Blocks []block
	Stop   string
	Status int // an error status instead of an answer
}

func text(s string) block { return block{"type": "text", "text": s} }
func thinking(s string) block {
	return block{"type": "thinking", "thinking": s, "signature": "ZmFrZS1zaWduYXR1cmU="}
}
func tool(id, name string, input map[string]any) block {
	return block{"type": "tool_use", "id": id, "name": name, "input": input}
}

// scripts: the steps for each word; the n-th step answers after n tool results.
func scripts(cwd string) map[string][]Step {
	return map[string][]Step{
		"HELLO":  {{[]block{text("Hello from the fake model.")}, "end_turn", 0}},
		"RENDER": {{[]block{text("Rendered `renders/demo.mp4` (3 s). The guide is in docs/guide.pdf.")}, "end_turn", 0}},
		"LIST": {
			{[]block{text("I'll list the files."), tool("toolu_fake_list", "Bash", map[string]any{"command": "ls", "description": "List files"})}, "tool_use", 0},
			{[]block{text("There are two files: notes.txt and todo.txt.")}, "end_turn", 0},
		},
		"EDIT": {
			{[]block{tool("toolu_fake_write", "Write", map[string]any{"file_path": strings.TrimRight(cwd, "/") + "/hello.txt", "content": "hello\n"})}, "tool_use", 0},
			{[]block{text("Wrote hello.txt.")}, "end_turn", 0},
		},
		"ASK": {
			{[]block{tool("toolu_fake_ask", "AskUserQuestion", map[string]any{"questions": []any{map[string]any{
				"question": "Which color should the button be?", "header": "Color", "multiSelect": false,
				"options": []any{map[string]any{"label": "Red", "description": "Warm"}, map[string]any{"label": "Blue", "description": "Cool"}},
			}}})}, "tool_use", 0},
			{[]block{text("Noted your choice.")}, "end_turn", 0},
		},
		"SLOW": {
			{[]block{tool("toolu_fake_slow", "Bash", map[string]any{"command": "python3 -c 'import time; time.sleep(60)'", "description": "Wait a minute"})}, "tool_use", 0},
			{[]block{text("Done waiting.")}, "end_turn", 0},
		},
		"THINK": {{[]block{thinking("The notes and the todo list may disagree.\nRead both before planning."), text("Plan: read both files, then merge them.")}, "end_turn", 0}},
		"LOOK": {
			{[]block{tool("toolu_fake_look", "Read", map[string]any{"file_path": strings.TrimRight(cwd, "/") + "/shot.png"})}, "tool_use", 0},
			{[]block{text("It is a blue square.")}, "end_turn", 0},
		},
		"FIND": {
			{[]block{tool("toolu_fake_grep", "Grep", map[string]any{"pattern": "TODO", "path": cwd})}, "tool_use", 0},
			{[]block{tool("toolu_fake_glob", "Glob", map[string]any{"pattern": "*.txt"})}, "tool_use", 0},
			{[]block{text("No TODO markers; two text files.")}, "end_turn", 0},
		},
		"HELP": {
			{[]block{tool("toolu_fake_agent", "Agent", map[string]any{"description": "Ask a helper", "prompt": "Say HELLO to the team.", "subagent_type": "general-purpose"})}, "tool_use", 0},
			{[]block{text("The helper said hello.")}, "end_turn", 0},
		},
		"WRITE": {{[]block{text("## Notes\n\n- **notes.txt** has one line\n- *todo.txt* has one line\n\n```sh\ncat notes.txt todo.txt\n```\n\n| file | lines |\n|---|---|\n| notes.txt | 1 |\n| todo.txt | 1 |\n\nSee [the guide](https://example.com/guide).")}, "end_turn", 0}},
		"FAIL":  {{Status: 400}},
		"CHANGE": {
			{[]block{tool("toolu_fake_read", "Read", map[string]any{"file_path": strings.TrimRight(cwd, "/") + "/notes.txt"})}, "tool_use", 0},
			{[]block{tool("toolu_fake_edit", "Edit", map[string]any{"file_path": strings.TrimRight(cwd, "/") + "/notes.txt", "old_string": "a\n", "new_string": "a\nb\nc\n"})}, "tool_use", 0},
			{[]block{text("Added two lines to notes.txt.")}, "end_turn", 0},
		},
		"BREAK": {
			{[]block{tool("toolu_fake_break", "Bash", map[string]any{"command": "echo checking; echo 'one test failed' >&2; exit 3", "description": "Run the check"})}, "tool_use", 0},
			{[]block{text("The check failed with code 3.")}, "end_turn", 0},
		},
	}
}

var word = regexp.MustCompile(`\b(HELLO|LIST|EDIT|ASK|SLOW|THINK|LOOK|FIND|HELP|WRITE|FAIL|CHANGE|BREAK|RENDER)\b`)
var cwdLine = regexp.MustCompile(`Primary working directory: ([^\s\\"]+)`)

// Server answers /v1/messages. Requests lists the paths it was asked for, for a test to look at.
type Server struct {
	mu       sync.Mutex
	Requests []string
	// Cwd is where EDIT writes when the request doesn't say.
	Cwd string
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.Requests = append(s.Requests, r.Method+" "+r.URL.Path)
	s.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	switch {
	case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/v1/messages/count_tokens"):
		writeJSON(w, map[string]any{"input_tokens": 10})
	case r.Method == "POST" && strings.HasSuffix(r.URL.Path, "/v1/messages"):
		var req struct {
			Model    string            `json:"model"`
			Stream   bool              `json:"stream"`
			Messages []json.RawMessage `json:"messages"`
			System   json.RawMessage   `json:"system"`
			Tools    []json.RawMessage `json:"tools"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			http.Error(w, `{"type":"error","error":{"type":"invalid_request_error","message":"bad json"}}`, 400)
			return
		}
		st := s.answer(req.Messages, string(req.System), len(req.Tools) > 0)
		if st.Status != 0 {
			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(st.Status)
			fmt.Fprint(w, `{"type":"error","error":{"type":"invalid_request_error","message":"The fake model refuses this request."}}`)
			return
		}
		if req.Stream {
			stream(w, req.Model, st)
		} else {
			writeJSON(w, message(req.Model, st))
		}
	default:
		writeJSON(w, map[string]any{})
	}
}

// answer picks the step: the word in the last prompt, and how many tool results came after it.
func (s *Server) answer(msgs []json.RawMessage, system string, tools bool) Step {
	ok := Step{[]block{text("OK.")}, "end_turn", 0}
	if !tools {
		return ok
	}
	cwd := s.Cwd
	if m := cwdLine.FindStringSubmatch(system); m != nil {
		cwd = m[1]
	}
	key, results := "", 0
	for i := len(msgs) - 1; i >= 0 && key == ""; i-- {
		var m struct {
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(msgs[i], &m) != nil || m.Role != "user" {
			continue
		}
		var parts []struct {
			Type string `json:"type"`
			Text string `json:"text"`
		}
		var str string
		if json.Unmarshal(m.Content, &str) == nil {
			parts = append(parts, struct {
				Type string `json:"type"`
				Text string `json:"text"`
			}{"text", str})
		} else {
			_ = json.Unmarshal(m.Content, &parts)
		}
		for _, p := range parts {
			if p.Type == "tool_result" {
				results++
			}
		}
		for _, p := range parts {
			if p.Type == "text" && !strings.HasPrefix(strings.TrimSpace(p.Text), "<system-reminder>") {
				if w := word.FindString(p.Text); w != "" {
					key = w
				}
			}
		}
	}
	steps := scripts(cwd)[key]
	if key == "" || results >= len(steps) {
		return ok
	}
	return steps[results]
}

var ids struct {
	sync.Mutex
	n int
}

func nextID() string {
	ids.Lock()
	defer ids.Unlock()
	ids.n++
	return fmt.Sprintf("msg_fake_%04d", ids.n)
}

func message(model string, st Step) map[string]any {
	return map[string]any{
		"id": nextID(), "type": "message", "role": "assistant", "model": model, "content": st.Blocks,
		"stop_reason": st.Stop, "stop_sequence": nil, "usage": map[string]any{"input_tokens": 10, "output_tokens": 5},
	}
}

func stream(w http.ResponseWriter, model string, st Step) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	f, _ := w.(http.Flusher)
	send := func(ev string, data any) {
		b, _ := json.Marshal(data)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev, b)
		if f != nil {
			f.Flush()
		}
	}
	send("message_start", map[string]any{"type": "message_start", "message": map[string]any{
		"id": nextID(), "type": "message", "role": "assistant", "model": model, "content": []any{},
		"stop_reason": nil, "stop_sequence": nil, "usage": map[string]any{"input_tokens": 10, "output_tokens": 1},
	}})
	for i, b := range st.Blocks {
		switch b["type"] {
		case "text":
			send("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": block{"type": "text", "text": ""}})
			send("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": block{"type": "text_delta", "text": b["text"]}})
		case "thinking":
			send("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": block{"type": "thinking", "thinking": "", "signature": ""}})
			send("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": block{"type": "thinking_delta", "thinking": b["thinking"]}})
			send("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": block{"type": "signature_delta", "signature": b["signature"]}})
		case "tool_use":
			send("content_block_start", map[string]any{"type": "content_block_start", "index": i, "content_block": block{"type": "tool_use", "id": b["id"], "name": b["name"], "input": map[string]any{}}})
			in, _ := json.Marshal(b["input"])
			send("content_block_delta", map[string]any{"type": "content_block_delta", "index": i, "delta": block{"type": "input_json_delta", "partial_json": string(in)}})
		}
		send("content_block_stop", map[string]any{"type": "content_block_stop", "index": i})
	}
	send("message_delta", map[string]any{"type": "message_delta", "delta": map[string]any{"stop_reason": st.Stop, "stop_sequence": nil}, "usage": map[string]any{"output_tokens": 5}})
	send("message_stop", map[string]any{"type": "message_stop"})
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

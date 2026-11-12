// Package fakeresponses is a stand-in for OpenAI's Responses API, for tests only: an installed agent runs against it
// (a model provider with its base URL pointing here, in a throwaway home), so its session records come from the real
// program with made-up words, no login and no tokens. The Codex, OpenCode and Pi readers' fixtures are made this way.
//
// The answer depends on a word in the person's last prompt:
//
//	HELLO  one sentence
//	LIST   a sentence, a command (ls), then after its result a sentence
//	EDIT   a new file hello.txt, then a sentence
//	CHANGE notes.txt's one line made three, then a sentence
//	BREAK  a command that prints two lines and exits 3, then a sentence
//	THINK  a reasoning summary, then a sentence
//	LOOK   a look at shot.png, then a sentence
//	RENDER a sentence naming the files it made: a video and a PDF, by paths in the session's folder
//	WORK   three commands of 6 s each, a sentence before each, then a sentence: time to send a message while it works
//	ROWS   a list of 150 numbered rows ("- acme-api line 1" …), long enough to scroll
//
// Anything else gets "OK.". Each step is a call of the tools the request offers: Codex's (exec_command, apply_patch,
// view_image), OpenCode 1's (bash, apply_patch, read with filePath), OpenCode 2's (shell, patch, read with path) or
// Pi's (bash, write, edit, read with path). Codex
// offers apply_patch only to some model names; the tests use "gpt-5.5", which is only the name the requests carry.
package fakeresponses

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

type item = map[string]any

func say(s string) item {
	return item{"type": "message", "role": "assistant", "status": "completed",
		"content": []item{{"type": "output_text", "text": s, "annotations": []any{}}}}
}

func call(id, name string, args map[string]any) item {
	b, _ := json.Marshal(args)
	return item{"type": "function_call", "call_id": id, "name": name, "arguments": string(b), "status": "completed"}
}

func reasoning(s string) item {
	return item{"type": "reasoning", "summary": []item{{"type": "summary_text", "text": s}}}
}

// A step a script asks for, put into a program's tools by its dialect.
type step struct {
	say, think string
	run        string // a command
	add        string // a new file, with addText
	addText    string
	change     string // a file whose oldText becomes newText
	old, new   string
	look       string // an image file
	id         string // the call's id
}

type dialect int

const (
	codex dialect = iota
	opencode
	opencode2
	pi
)

// dialectOf: the program by the tools its request offers.
func dialectOf(tools []json.RawMessage) dialect {
	names := map[string]bool{}
	for _, t := range tools {
		var x struct {
			Name string `json:"name"`
		}
		json.Unmarshal(t, &x)
		names[x.Name] = true
	}
	switch {
	case names["exec_command"]:
		return codex
	case names["write"] && names["edit"]:
		return pi
	case names["shell"] && names["patch"]:
		return opencode2
	case names["bash"]:
		return opencode
	}
	return codex
}

func patchText(st step) string {
	if st.add != "" {
		return "*** Begin Patch\n*** Add File: " + st.add + "\n+" + strings.TrimSuffix(st.addText, "\n") + "\n*** End Patch\n"
	}
	body := ""
	for _, l := range strings.Split(strings.TrimSuffix(st.old, "\n"), "\n") {
		body += " " + l + "\n"
	}
	for _, l := range strings.Split(strings.TrimSuffix(strings.TrimPrefix(st.new, st.old), "\n"), "\n") {
		body += "+" + l + "\n"
	}
	return "*** Begin Patch\n*** Update File: " + st.change + "\n@@\n" + body + "*** End Patch\n"
}

// items: one step in a dialect's tools.
func (st step) items(d dialect) []item {
	var out []item
	if st.think != "" {
		out = append(out, reasoning(st.think))
	}
	if st.say != "" {
		out = append(out, say(st.say))
	}
	switch {
	case st.run != "":
		switch d {
		case codex:
			out = append(out, call(st.id, "exec_command", map[string]any{"cmd": st.run}))
		case opencode2:
			out = append(out, call(st.id, "shell", map[string]any{"command": st.run}))
		default:
			out = append(out, call(st.id, "bash", map[string]any{"command": st.run}))
		}
	case st.look != "":
		switch d {
		case codex:
			out = append(out, call(st.id, "view_image", map[string]any{"path": st.look}))
		case opencode:
			out = append(out, call(st.id, "read", map[string]any{"filePath": st.look}))
		default:
			out = append(out, call(st.id, "read", map[string]any{"path": st.look}))
		}
	case st.add != "" || st.change != "":
		switch d {
		case codex:
			out = append(out, item{"type": "custom_tool_call", "call_id": st.id, "name": "apply_patch", "input": patchText(st), "status": "completed"})
		case opencode:
			out = append(out, call(st.id, "apply_patch", map[string]any{"patchText": patchText(st)}))
		case opencode2:
			out = append(out, call(st.id, "patch", map[string]any{"patchText": patchText(st)}))
		default:
			if st.add != "" {
				out = append(out, call(st.id, "write", map[string]any{"path": st.add, "content": st.addText}))
			} else {
				out = append(out, call(st.id, "edit", map[string]any{"path": st.change, "edits": []any{map[string]any{"oldText": st.old, "newText": st.new}}}))
			}
		}
	}
	return out
}

// scripts: the steps for each word; the n-th answers after n tool results.
var scripts = map[string][]step{
	"HELLO": {{say: "Hello from the fake model."}},
	"LIST":  {{say: "I'll list the files.", run: "ls", id: "call_fake_list"}, {say: "There are two files here."}},
	"EDIT":  {{add: "hello.txt", addText: "hello\n", id: "call_fake_edit"}, {say: "I wrote hello.txt."}},
	"CHANGE": {{change: "notes.txt", old: "a\n", new: "a\nb\nc\n", id: "call_fake_change"},
		{say: "Added two lines to notes.txt."}},
	"BREAK": {{run: "echo checking; echo 'one test failed' >&2; exit 3", id: "call_fake_break"},
		{say: "The check failed with code 3."}},
	"THINK": {{think: "Weighing two plans: the short one first.", say: "The short plan is better."}},
	"LOOK":  {{look: "shot.png", id: "call_fake_look"}, {say: "It is a blue square."}},
	"WORK": {{say: "Step one.", run: "python3 -c 'import time; time.sleep(6)'", id: "call_fake_w1"}, {say: "Step two.", run: "python3 -c 'import time; time.sleep(6)'", id: "call_fake_w2"},
		{say: "Step three.", run: "python3 -c 'import time; time.sleep(6)'", id: "call_fake_w3"}, {say: "All three steps are done."}},
	"RENDER": {{say: "Rendered `renders/demo.mp4` (3 s). The guide is in docs/guide.pdf."}},
	"ROWS":   {{say: Rows(150)}},
}

var word = regexp.MustCompile(`\b(HELLO|LIST|EDIT|CHANGE|BREAK|THINK|LOOK|RENDER|WORK|ROWS)\b`)

// Server answers POST /v1/responses as a stream. Requests lists what it was asked, for a test to look at.
type Server struct {
	mu       sync.Mutex
	Requests []string
	n        int
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.Requests = append(s.Requests, r.Method+" "+r.URL.Path)
	s.n++
	n := s.n
	s.mu.Unlock()
	if r.Method != "POST" || !strings.HasSuffix(r.URL.Path, "/responses") {
		http.NotFound(w, r)
		return
	}
	var req struct {
		Input []json.RawMessage `json:"input"`
		Tools []json.RawMessage `json:"tools"`
	}
	body, _ := io.ReadAll(r.Body)
	if json.Unmarshal(body, &req) != nil {
		http.Error(w, `{"error":{"message":"bad json"}}`, 400)
		return
	}
	stream(w, n, respond(req.Tools, req.Input))
}

type textPart struct {
	Type string `json:"type"`
	Text string `json:"text"`
}

// respond: the answer to a request, in the tools it offers.
func respond(tools, input []json.RawMessage) []item {
	key, n := answer(input)
	steps := scripts[key]
	if key == "" || n >= len(steps) {
		return []item{say("OK.")}
	}
	return steps[n].items(dialectOf(tools))
}

// answer: the word in the last prompt that has one, and how many tool results came after it.
func answer(input []json.RawMessage) (string, int) {
	key, results := "", 0
	for i := len(input) - 1; i >= 0 && key == ""; i-- {
		var it struct {
			Type    string          `json:"type"`
			Role    string          `json:"role"`
			Content json.RawMessage `json:"content"`
		}
		if json.Unmarshal(input[i], &it) != nil {
			continue
		}
		switch {
		case it.Type == "function_call_output" || it.Type == "custom_tool_call_output":
			results++
		// A message may come with its role only, and its content as a plain string.
		case (it.Type == "message" || it.Type == "") && it.Role == "user":
			var parts []textPart
			var s string
			if json.Unmarshal(it.Content, &s) == nil {
				parts = []textPart{{"input_text", s}}
			} else {
				json.Unmarshal(it.Content, &parts)
			}
			for _, c := range parts {
				// Codex sends its own context as user messages too, each in one tag.
				if c.Type == "input_text" && !strings.HasPrefix(strings.TrimSpace(c.Text), "<") {
					key = word.FindString(c.Text)
				}
			}
		}
	}
	return key, results
}

// stream writes a response as Server-Sent Events: each item added then done, then the response completed.
func stream(w http.ResponseWriter, n int, items []item) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	f, _ := w.(http.Flusher)
	send := func(ev map[string]any) {
		b, _ := json.Marshal(ev)
		fmt.Fprintf(w, "event: %s\ndata: %s\n\n", ev["type"], b)
		if f != nil {
			f.Flush()
		}
	}
	id := fmt.Sprintf("resp_fake_%d", n)
	send(map[string]any{"type": "response.created", "response": map[string]any{"id": id}})
	for i, it := range items {
		done := item{}
		for k, v := range it {
			done[k] = v
		}
		done["id"] = fmt.Sprintf("item_fake_%d_%d", n, i)
		send(map[string]any{"type": "response.output_item.added", "output_index": i, "item": done})
		if done["type"] == "message" {
			text := done["content"].([]item)[0]["text"]
			send(map[string]any{"type": "response.output_text.delta", "output_index": i, "content_index": 0, "item_id": done["id"], "delta": text})
		}
		send(map[string]any{"type": "response.output_item.done", "output_index": i, "item": done})
	}
	send(map[string]any{"type": "response.completed", "response": map[string]any{"id": id, "usage": map[string]any{
		"input_tokens": 10, "input_tokens_details": map[string]any{"cached_tokens": 0},
		"output_tokens": 5, "output_tokens_details": map[string]any{"reasoning_tokens": 0}, "total_tokens": 15}}})
}

// Rows is ROWS's answer: n rows of a list, "- acme-api line 1" to "- acme-api line n". A list keeps one row per line
// in every program's Markdown; plain lines would run together into one paragraph.
func Rows(n int) string {
	var b strings.Builder
	for i := 1; i <= n; i++ {
		if i > 1 {
			b.WriteByte('\n')
		}
		fmt.Fprintf(&b, "- acme-api line %d", i)
	}
	return b.String()
}

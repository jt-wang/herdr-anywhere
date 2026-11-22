// Package fakegemini is a stand-in for the Gemini API, for tests only: the installed Antigravity CLI (agy) runs
// against it as an enterprise model gateway (AGY_LLM_GATEWAY_URL on localhost) under a throwaway HOME, so its
// transcripts come from the real program with made-up words, no sign-in and no tokens. The reader's fixtures and
// the live check are made this way.
//
// The answer depends on a word in the person's last prompt:
//
//	HELLO  one sentence
//	LIST   a sentence, a run_command (ls), then a sentence
//	CHANGE a read_file of notes.txt, an edit_file of it (one line becomes three), then a sentence
//	WRITE  a write_file of a new file (hello.txt), then a sentence
//	BREAK  a run_command that prints two lines and exits 3, then a sentence
//	ASK    an ask_question call with two questions ("(Recommended)" first option; the second multi-select)
//	PICK   the same with the multi-select question first, and "(Recommended)" after a label
//	THINK  a thought with its text, then a sentence
//	HELP   an invoke_subagent call whose prompt says HELLO, then a sentence
//	MARK   a reply in Markdown: a heading, a list, code, a table, a link
//	LONG   a reply of 120 numbered lines, long enough for agy to truncate it in transcript.jsonl
//	PLAN   (in plan mode, /plan) a sentence and a read_file, then a write_file of implementation_plan.md in the
//	       conversation's artifact folder that asks for review (ArtifactMetadata.RequestFeedback), then a sentence
//	       After agy's review ("[Approved] implementation_plan.md"): an edit_file of notes.txt, then a sentence
//	WORK   three run_command calls of 6 s each, a sentence before each: time to send a message while it works
//	SLOW   a run_command that takes 60 s (to interrupt)
//	FAIL   an API error (400), as a refused request
//
// Anything else (side requests such as titles, prompts with none of these words) gets "OK.".
package fakegemini

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"regexp"
	"strings"
	"sync"
)

type part = map[string]any

// Step is one answer of a script: the model's parts.
type Step []part

// failing is the step FAIL answers: an error status instead of an answer.
var failing = Step{part{"fail": true}}

func text(s string) part    { return part{"text": s} }
func thought(s string) part { return part{"text": s, "thought": true} }
func call(name string, args map[string]any) part {
	return part{"functionCall": map[string]any{"name": name, "args": args}, "thoughtSignature": "ZmFrZS1zaWduYXR1cmU="}
}

func run(cmd, summary string) part {
	return call("run_command", map[string]any{"CommandLine": cmd, "toolSummary": summary, "WaitMsBeforeAsync": 5000})
}

// Rows is a reply of n numbered lines.
func Rows(n int) string {
	var b strings.Builder
	b.WriteString("Here are the rows:\n\n")
	for i := 1; i <= n; i++ {
		fmt.Fprintf(&b, "- row %d of the sample list, with some words to make it longer than a short line\n", i)
	}
	return b.String()
}

// scripts: the steps for each word; the n-th step answers after n tool results.
func scripts(cwd, artifacts string) map[string][]Step {
	at := func(f string) string { return strings.TrimRight(cwd, "/") + "/" + f }
	return map[string][]Step{
		"HELLO": {{text("Hello from the fake model.")}},
		"LIST": {
			{text("I'll list the files."), run("ls", "File listing")},
			{text("There are two files: notes.txt and todo.txt.")},
		},
		"CHANGE": {
			{call("read_file", map[string]any{"Path": at("notes.txt")})},
			{call("edit_file", map[string]any{"Path": at("notes.txt"), "Target": "a\n", "Replacement": "a\nb\nc\n"})},
			{text("Added two lines to notes.txt.")},
		},
		"WRITE": {
			{call("write_file", map[string]any{"Path": at("hello.txt"), "Content": "hello\nworld\n"})},
			{text("Wrote hello.txt.")},
		},
		"BREAK": {
			{run("echo checking; echo 'one test failed' >&2; exit 3", "Check run")},
			{text("The check failed with code 3.")},
		},
		"ASK": {
			{call("ask_question", map[string]any{"questions": []any{
				map[string]any{"question": "Which color should the button be?", "options": []any{"(Recommended) Blue", "Red"}},
				map[string]any{"question": "Which sizes should it come in?", "is_multi_select": true, "options": []any{"Small", "Large"}},
			}})},
			{text("Noted your choice.")},
		},
		"PICK": {
			{call("ask_question", map[string]any{"questions": []any{
				map[string]any{"question": "Which toppings?", "is_multi_select": true, "options": []any{"Cheese", "Olives"}},
				map[string]any{"question": "Which crust?", "options": []any{"Thin", "Thick (Recommended)"}},
			}})},
			{text("Noted your order.")},
		},
		"THINK": {{thought("The notes and the todo list may disagree.\nRead both before planning."), text("Plan: read both files, then merge them.")}},
		"HELP": {
			{call("invoke_subagent", map[string]any{"TypeName": "research", "Role": "Greeting Helper", "Prompt": "Say HELLO to the team."})},
			{text("The helper said hello.")},
		},
		"MARK": {{text("## Notes\n\n- **notes.txt** has one line\n- *todo.txt* has one line\n\n```sh\ncat notes.txt todo.txt\n```\n\n| file | lines |\n|---|---|\n| notes.txt | 1 |\n| todo.txt | 1 |\n\nSee [the guide](https://example.com/guide).")}},
		"LONG": {{text(Rows(120))}},
		"PLAN": {
			{text("I'll read the notes first."), call("read_file", map[string]any{"Path": at("notes.txt")})},
			{call("write_file", map[string]any{"Path": strings.TrimRight(artifacts, "/") + "/implementation_plan.md", "Content": Plan,
				"ArtifactMetadata": map[string]any{"Summary": "Merge todo.txt into notes.txt.", "UserFacing": true, "RequestFeedback": true}})},
			{text("The plan is ready for your review.")},
		},
		"APPROVED": {
			{text("Carrying out the plan."), call("edit_file", map[string]any{"Path": at("notes.txt"), "Target": "a\n", "Replacement": "a\nbuy milk\n"})},
			{text("Merged the notes.")},
		},
		"WORK": {
			{text("Step one."), run("python3 -c 'import time; time.sleep(6)'", "First step")},
			{text("Step two."), run("python3 -c 'import time; time.sleep(6)'", "Second step")},
			{text("Step three."), run("python3 -c 'import time; time.sleep(6)'", "Third step")},
			{text("All three steps are done.")},
		},
		"FAIL": {failing},
		"SLOW": {
			{run("python3 -c 'import time; time.sleep(60)'", "Minute wait")},
			{text("Done waiting.")},
		},
	}
}

var word = regexp.MustCompile(`\b(HELLO|LIST|CHANGE|WRITE|BREAK|ASK|PICK|THINK|HELP|MARK|LONG|PLAN|WORK|SLOW|FAIL)\b`)

// The folder agy runs commands in, as its system prompt says it.
var cwdLine = regexp.MustCompile(`Command Working Directory: ([^\n\\"]+)`)

// The conversation's artifact folder, as agy's system prompt says it.
var artifactLine = regexp.MustCompile(`Artifact Directory Path: ([^\n\\"]+)`)

// Plan is the plan PLAN writes.
const Plan = "# Merge the notes\n\n1. Read notes.txt and todo.txt.\n2. Put the todo lines under the notes.\n3. Delete todo.txt.\n"

// Model is the one model the fake gateway lists.
const Model = "fake-model"

// Server answers the Gemini API's model list and streamGenerateContent. Requests lists what it was asked, for a
// test to look at.
type Server struct {
	mu       sync.Mutex
	Requests []string
	// Tools are the tool names of the last request that had any.
	Tools []string
	// Cwd is where the scripts' files are when the request doesn't say.
	Cwd string
}

type content struct {
	Role  string `json:"role"`
	Parts []struct {
		Text             string          `json:"text"`
		Thought          bool            `json:"thought"`
		FunctionCall     json.RawMessage `json:"functionCall"`
		FunctionResponse json.RawMessage `json:"functionResponse"`
	} `json:"parts"`
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	s.mu.Lock()
	s.Requests = append(s.Requests, r.Method+" "+r.URL.Path)
	s.mu.Unlock()
	body, _ := io.ReadAll(r.Body)
	switch {
	case r.Method == "GET" && strings.HasSuffix(r.URL.Path, "/models"):
		writeJSON(w, map[string]any{"models": []any{map[string]any{
			"name": "models/" + Model, "displayName": "Fake Model", "inputTokenLimit": 1048576, "outputTokenLimit": 65535,
			"supportedGenerationMethods": []string{"generateContent", "streamGenerateContent", "countTokens"},
		}}})
	case r.Method == "POST" && strings.Contains(r.URL.Path, ":countTokens"):
		writeJSON(w, map[string]any{"totalTokens": 10})
	case r.Method == "POST" && (strings.Contains(r.URL.Path, ":streamGenerateContent") || strings.Contains(r.URL.Path, ":generateContent")):
		var req struct {
			Contents          []content         `json:"contents"`
			SystemInstruction *content          `json:"systemInstruction"`
			Tools             []json.RawMessage `json:"tools"`
		}
		if err := json.Unmarshal(body, &req); err != nil {
			w.WriteHeader(400)
			writeJSON(w, map[string]any{"error": map[string]any{"code": 400, "message": "bad json", "status": "INVALID_ARGUMENT"}})
			return
		}
		system := ""
		if req.SystemInstruction != nil {
			for _, p := range req.SystemInstruction.Parts {
				system += p.Text
			}
		}
		var names []string
		for _, t := range req.Tools {
			var d struct {
				Decls []struct {
					Name string `json:"name"`
				} `json:"functionDeclarations"`
			}
			json.Unmarshal(t, &d)
			for _, x := range d.Decls {
				names = append(names, x.Name)
			}
		}
		if len(names) > 0 {
			s.mu.Lock()
			s.Tools = names
			s.mu.Unlock()
		}
		st := s.answer(req.Contents, system, len(req.Tools) > 0)
		if len(st) == 1 && st[0]["fail"] == true {
			w.WriteHeader(400)
			writeJSON(w, map[string]any{"error": map[string]any{"code": 400, "message": "The fake model refuses this request.", "status": "INVALID_ARGUMENT"}})
			return
		}
		if strings.Contains(r.URL.Path, ":streamGenerateContent") {
			stream(w, st)
		} else {
			writeJSON(w, response(st))
		}
	default:
		writeJSON(w, map[string]any{})
	}
}

// answer picks the step: the word in the last prompt, and how many tool results came after it.
func (s *Server) answer(cs []content, system string, tools bool) Step {
	ok := Step{text("OK.")}
	if !tools {
		return ok
	}
	cwd := s.Cwd
	if m := cwdLine.FindStringSubmatch(system); m != nil {
		cwd = strings.TrimSpace(m[1])
	}
	key, results := "", 0
	for i := len(cs) - 1; i >= 0 && key == ""; i-- {
		if cs[i].Role != "user" {
			continue
		}
		for _, p := range cs[i].Parts {
			if len(p.FunctionResponse) > 0 {
				results++
			}
		}
		for _, p := range cs[i].Parts {
			// Only what the person typed: agy wraps it in <USER_REQUEST>.
			if _, req, ok := strings.Cut(p.Text, "<USER_REQUEST>"); ok {
				req, _, _ = strings.Cut(req, "</USER_REQUEST>")
				if w := word.FindString(req); w != "" {
					key = w
				}
				// The plan approved in agy's /artifact review (its prompt is "[Approved] <file>"): carry it out.
				if strings.HasPrefix(strings.TrimSpace(req), "[Approved]") {
					key = "APPROVED"
				}
			}
		}
	}
	artifacts := cwd
	if m := artifactLine.FindStringSubmatch(system); m != nil {
		artifacts = strings.TrimSpace(m[1])
	}
	steps := scripts(cwd, artifacts)[key]
	if key == "" || results >= len(steps) {
		return ok
	}
	return steps[results]
}

func response(st Step) map[string]any {
	return map[string]any{
		"candidates":    []any{map[string]any{"content": map[string]any{"role": "model", "parts": st}, "finishReason": "STOP", "index": 0}},
		"usageMetadata": map[string]any{"promptTokenCount": 10, "candidatesTokenCount": 5, "totalTokenCount": 15},
		"modelVersion":  Model,
	}
}

func stream(w http.ResponseWriter, st Step) {
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	f, _ := w.(http.Flusher)
	b, _ := json.Marshal(response(st))
	fmt.Fprintf(w, "data: %s\r\n\r\n", b)
	if f != nil {
		f.Flush()
	}
}

func writeJSON(w http.ResponseWriter, v any) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(v)
}

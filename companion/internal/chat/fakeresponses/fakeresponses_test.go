package fakeresponses

import (
	"encoding/json"
	"testing"
)

func text(items []item) string {
	b, _ := json.Marshal(items)
	return string(b)
}

// A prompt finds its answer whether the client marks it as a message or, as the Responses API allows, sends only its
// role (Pi and OpenCode do).
func TestAnswerFindsTheWord(t *testing.T) {
	for _, in := range []string{
		`[{"type":"message","role":"user","content":[{"type":"input_text","text":"HELLO there"}]}]`,
		`[{"role":"user","content":[{"type":"input_text","text":"HELLO there"}]}]`,
		`[{"role":"user","content":"HELLO there"}]`,
	} {
		var input []json.RawMessage
		if err := json.Unmarshal([]byte(in), &input); err != nil {
			t.Fatal(err)
		}
		if got, want := text(respond(nil, input)), `[{"content":[{"annotations":[],"text":"Hello from the fake model.","type":"output_text"}],"role":"assistant","status":"completed","type":"message"}]`; got != want {
			t.Errorf("%s: got %s", in, got)
		}
	}
}

func tools(names ...string) []json.RawMessage {
	var out []json.RawMessage
	for _, n := range names {
		out = append(out, json.RawMessage(`{"type":"function","name":"`+n+`"}`))
	}
	return out
}

func prompt(s string) []json.RawMessage {
	return []json.RawMessage{json.RawMessage(`{"role":"user","content":[{"type":"input_text","text":"` + s + `"}]}`)}
}

func callOf(t *testing.T, items []item) (string, map[string]any) {
	t.Helper()
	for _, it := range items {
		switch it["type"] {
		case "function_call":
			var args map[string]any
			json.Unmarshal([]byte(it["arguments"].(string)), &args)
			return it["name"].(string), args
		case "custom_tool_call":
			return it["name"].(string), map[string]any{"input": it["input"]}
		}
	}
	t.Fatalf("no tool call in %s", text(items))
	return "", nil
}

// Each program gets the step in its own tools: Codex's (unchanged, as its fixtures were recorded), OpenCode 1's and
// 2's, and Pi's.
func TestStepsInEachProgramsTools(t *testing.T) {
	codex := tools("exec_command", "view_image")
	opencode := tools("apply_patch", "bash", "read", "glob")
	pi := tools("read", "bash", "edit", "write")
	opencode2 := tools("glob", "patch", "read", "shell", "subagent")
	cases := []struct {
		tools []json.RawMessage
		word  string
		name  string
		arg   string
		want  any
	}{
		{codex, "LIST", "exec_command", "cmd", "ls"},
		{codex, "LOOK", "view_image", "path", "shot.png"},
		{codex, "EDIT", "apply_patch", "input", "*** Begin Patch\n*** Add File: hello.txt\n+hello\n*** End Patch\n"},
		{opencode, "LIST", "bash", "command", "ls"},
		{opencode, "LOOK", "read", "filePath", "shot.png"},
		{opencode, "EDIT", "apply_patch", "patchText", "*** Begin Patch\n*** Add File: hello.txt\n+hello\n*** End Patch\n"},
		{pi, "LIST", "bash", "command", "ls"},
		{pi, "LOOK", "read", "path", "shot.png"},
		{pi, "EDIT", "write", "content", "hello\n"},
		{pi, "CHANGE", "edit", "path", "notes.txt"},
		{opencode2, "LIST", "shell", "command", "ls"},
		{opencode2, "LOOK", "read", "path", "shot.png"},
		{opencode2, "EDIT", "patch", "patchText", "*** Begin Patch\n*** Add File: hello.txt\n+hello\n*** End Patch\n"},
	}
	for _, c := range cases {
		name, args := callOf(t, respond(c.tools, prompt(c.word+" it")))
		if name != c.name || args[c.arg] != c.want {
			t.Errorf("%s with %s: %s %v", c.word, c.name, name, args)
		}
	}
	// Codex's call ids and texts stay as recorded.
	got := text(respond(codex, prompt("LIST the files here")))
	want := `[{"content":[{"annotations":[],"text":"I'll list the files.","type":"output_text"}],"role":"assistant","status":"completed","type":"message"},{"arguments":"{\"cmd\":\"ls\"}","call_id":"call_fake_list","name":"exec_command","status":"completed","type":"function_call"}]`
	if got != want {
		t.Errorf("Codex LIST changed:\n%s", got)
	}
}

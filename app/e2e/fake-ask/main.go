// A stand-in for Claude Code 2.1.325 asking: it shows screens Claude Code drew in a 46-column terminal (e2e/files/ask,
// from `hc read`, made-up words and model), reports its status as Claude Code does (OSC 7501 with kind, so herdr-core
// says question or permission), and goes to the next screen on the keys Claude Code takes there, as measured. It writes
// the pid file and the session record Claude Code writes (under CLAUDE_CONFIG_DIR), from records Claude Code 2.1.325
// wrote against a fake API (beside the flow; the reader's fixtures claude-code/2.1.325/ask*.jsonl), so the Companion
// finds the conversation and what was asked.
// Usage: fake-ask FLOW.json   (e2e/files/ask/question.json or permission.json; its screens and records beside it)
// Built with `GO111MODULE=off go build`, as e2e/fake-claude.
package main

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

type state struct {
	Screen string            `json:"screen"`
	State  string            `json:"state"`
	Kind   string            `json:"kind"`
	Keys   map[string]string `json:"keys"`
	Record int               `json:"record"` // the fixture whose lines past the ones written so far go into the record here
	Msg    string            `json:"msg"`    // what it asks here, when not the flow's (a second call's questions)
}

type flow struct {
	Fixtures []string         `json:"fixtures"`
	Start    string           `json:"start"`
	States   map[string]state `json:"states"`
	Msg      string           `json:"msg"`
}

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "usage: fake-ask FLOW.json")
		os.Exit(2)
	}
	dir := filepath.Dir(os.Args[1])
	var f flow
	b, err := os.ReadFile(os.Args[1])
	must(err)
	must(json.Unmarshal(b, &f))
	cfg := os.Getenv("CLAUDE_CONFIG_DIR")
	cwd, _ := os.Getwd()
	pid := os.Getpid()
	sid := fmt.Sprintf("fa%08x-0000-4000-8000-%012x", pid, time.Now().UnixNano()&0xffffffffffff)
	start, _ := exec.Command("env", "TZ=UTC", "ps", "-o", "lstart=", "-p", fmt.Sprint(pid)).Output()
	must(os.MkdirAll(filepath.Join(cfg, "sessions"), 0o755))
	pidfile := filepath.Join(cfg, "sessions", fmt.Sprintf("%d.json", pid))
	pf, _ := json.Marshal(map[string]any{"pid": pid, "sessionId": sid, "cwd": cwd, "procStart": strings.TrimSpace(string(start)),
		"version": "2.1.325", "kind": "interactive", "entrypoint": "cli"})
	must(os.WriteFile(pidfile, pf, 0o644))
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, syscall.SIGTERM, syscall.SIGHUP, syscall.SIGINT)
	go func() { <-quit; os.Remove(pidfile); os.Exit(0) }()
	defer os.Remove(pidfile)

	record := filepath.Join(cfg, "projects", strings.NewReplacer("/", "-", ".", "-").Replace(cwd), sid+".jsonl")
	must(os.MkdirAll(filepath.Dir(record), 0o755))
	written := 0
	write := func(fixture int) { // the fixture's lines past the first `written`, as this session's
		lines := bytes.Split(bytes.TrimSpace(must2(os.ReadFile(filepath.Join(dir, f.Fixtures[fixture])))), []byte("\n"))
		out := must2(os.OpenFile(record, os.O_APPEND|os.O_CREATE|os.O_WRONLY, 0o644))
		defer out.Close()
		for _, l := range lines[min(written, len(lines)):] {
			var d map[string]any
			must(json.Unmarshal(l, &d))
			if c, ok := d["cwd"].(string); ok {
				l = bytes.ReplaceAll(l, []byte(c), []byte(cwd))
				must(json.Unmarshal(l, &d))
			}
			if _, ok := d["sessionId"]; ok {
				d["sessionId"] = sid
			}
			d["timestamp"] = time.Now().UTC().Format("2006-01-02T15:04:05.000Z")
			must2(out.Write(append(must2(json.Marshal(d)), '\n')))
		}
		written = len(lines)
	}
	write(0)

	// The terminal raw, so each key comes as it is pressed and nothing is echoed.
	stty := exec.Command("stty", "raw", "-echo")
	stty.Stdin = os.Stdin
	_ = stty.Run()
	at := f.Start
	show := func() {
		s := f.States[at]
		if s.Record > 0 {
			write(s.Record)
		}
		screen := must2(os.ReadFile(filepath.Join(dir, s.Screen)))
		fmt.Print("\x1b[H\x1b[2J" + strings.ReplaceAll(strings.TrimRight(string(screen), "\n"), "\n", "\r\n"))
		report := "state=" + s.State + ":app=claude-code"
		if s.Kind != "" {
			report += ":kind=" + s.Kind
		}
		msg := f.Msg
		if s.Msg != "" {
			msg = s.Msg
		}
		if s.State == "blocked" && msg != "" {
			report += ":msg=" + base64.StdEncoding.EncodeToString([]byte(msg))
		}
		fmt.Printf("\x1b]7501;%s\x07", report)
	}
	show()
	in := bufio.NewReader(os.Stdin)
	for {
		k, err := key(in)
		if err != nil {
			select {} // the terminal closed its input: wait to be stopped
		}
		if next, ok := f.States[at].Keys[k]; ok {
			at = next
			show()
		}
	}
}

// key reads one key press as herdr-core's terminal.keys names it: a digit, tab, enter, esc, up, down, left, right.
func key(in *bufio.Reader) (string, error) {
	c, err := in.ReadByte()
	if err != nil {
		return "", err
	}
	switch c {
	case '\t':
		return "tab", nil
	case '\r', '\n':
		return "enter", nil
	case 0x1b:
		if in.Buffered() == 0 {
			time.Sleep(30 * time.Millisecond)
		}
		if in.Buffered() >= 2 {
			if b, _ := in.Peek(2); b[0] == '[' || b[0] == 'O' {
				in.Discard(2)
				if n, ok := map[byte]string{'A': "up", 'B': "down", 'C': "right", 'D': "left"}[b[1]]; ok {
					return n, nil
				}
				return "", nil
			}
		}
		return "esc", nil
	}
	return string(c), nil
}

func must(err error) {
	if err != nil {
		fmt.Fprintln(os.Stderr, "fake-ask:", err)
		os.Exit(1)
	}
}

func must2[T any](v T, err error) T { must(err); return v }

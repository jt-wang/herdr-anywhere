// Command fakeagent stands in for Claude Code, Codex, Pi or OpenCode 2 in an e2e's isolated herdr-core, with no agent
// installed: it reports to herdr-core as the agent does (OSC 7501 with its app name; OpenCode also its session id
// while it works, as `id=`), keeps its record where the agent keeps it (replay), and for each line typed plays a
// recorded turn with the typed words as the prompt: the one whose prompt starts with the same word (LIST, EDIT, BREAK,
// LOOK, RENDER…), else the next. Pi's pane gets the labels Herdr Anywhere's Pi file sets.
//
// Usage: fakeagent -agent claude-code|codex|pi|opencode [-preload N] [-long N] [-delay D] [-hc PATH] RECORDING...
//
//	-preload N  a conversation of N turns is there from the start, prompts numbered "001 …"
//	-long N     the LIST command printed N invented lines
//	-delay D    between a turn's lines (default 400ms), so the phone sees a turn grow
//
// Where the record goes is the agent's own setting: CLAUDE_CONFIG_DIR, CODEX_HOME, PI_CODING_AGENT_DIR, or
// XDG_DATA_HOME (OpenCode). The folder is the one it is started in.
package main

import (
	"bufio"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"os/signal"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"

	"herdr-anywhere/companion/internal/testfake/replay"
)

var dirs = map[string]string{"claude-code": "CLAUDE_CONFIG_DIR", "codex": "CODEX_HOME", "pi": "PI_CODING_AGENT_DIR", "opencode": "XDG_DATA_HOME"}

func main() {
	agent := flag.String("agent", "", "claude-code, codex, pi or opencode")
	preload := flag.Int("preload", 0, "turns there from the start")
	long := flag.Int("long", 0, "lines the LIST command printed")
	delay := flag.Duration("delay", 400*time.Millisecond, "between a turn's lines")
	hc := flag.String("hc", "hc", "herdr-core's hc, for Pi's labels")
	flag.Parse()
	env, ok := dirs[*agent]
	if !ok || flag.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: fakeagent -agent claude-code|codex|pi|opencode [-preload N] [-long N] [-delay D] [-hc PATH] RECORDING...")
		os.Exit(2)
	}
	dir := os.Getenv(env)
	if dir == "" {
		fail("%s is not set: the fake writes only where the test says", env)
	}
	cwd, err := os.Getwd()
	if err != nil {
		fail("%v", err)
	}
	s, err := replay.Load(*agent, flag.Args()...)
	if err != nil {
		fail("%v", err)
	}
	o := replay.Options{Dir: dir, Cwd: cwd, Pid: os.Getpid(), Long: *long}
	if *agent == "claude-code" {
		// How Claude Code writes procStart (claude.Reader checks it): ps's lstart, in UTC.
		cmd := exec.Command("ps", "-o", "lstart=", "-p", strconv.Itoa(os.Getpid()))
		cmd.Env = append(os.Environ(), "TZ=UTC")
		out, err := cmd.Output()
		if err != nil {
			fail("ps: %v", err)
		}
		o.ProcStart = strings.TrimSpace(string(out))
	}
	p, err := s.Start(o)
	if err != nil {
		fail("%v", err)
	}
	stop := func() {
		p.Close()
		if *agent == "claude-code" {
			os.Remove(filepath.Join(dir, "sessions", strconv.Itoa(os.Getpid())+".json"))
		}
	}
	defer stop()
	sig := make(chan os.Signal, 1)
	signal.Notify(sig, syscall.SIGTERM, syscall.SIGHUP, os.Interrupt)
	go func() { <-sig; stop(); os.Exit(0) }()

	if *agent == "pi" {
		// What Herdr Anywhere's Pi file does at Pi's start (pi/herdr-anywhere.ts).
		if t := os.Getenv("HC_TERMINAL_ID"); t != "" {
			if out, err := exec.Command(*hc, "label", t, "herdr-anywhere.chat.pid="+strconv.Itoa(os.Getpid()),
				"herdr-anywhere.chat.session="+p.Path()).CombinedOutput(); err != nil {
				fail("hc label: %v: %s", err, out)
			}
		}
	}
	if *preload > 0 {
		if err := p.Preload(*preload); err != nil {
			fail("%v", err)
		}
	}
	report := func(state string) {
		fmt.Printf("\033]7501;state=%s:app=%s\033\\", state, *agent)
		if *agent == "opencode" && state == "working" {
			fmt.Printf("\033]7501;state=working:app=opencode:id=%s\033\\", p.Session())
		}
	}
	report("idle")
	in := bufio.NewScanner(os.Stdin)
	in.Buffer(make([]byte, 64<<10), 1<<20)
	for {
		fmt.Print("\r\n> ")
		if !in.Scan() {
			return
		}
		said := strings.TrimSpace(in.Text())
		if said == "" {
			continue
		}
		report("working")
		if err := p.PlayFor(said, *delay); err != nil {
			fail("%v", err)
		}
		fmt.Print("\r\n(the next recorded turn)\r\n")
		report("done")
	}
}

func fail(f string, a ...any) {
	fmt.Fprintf(os.Stderr, "fakeagent: "+f+"\n", a...)
	os.Exit(1)
}

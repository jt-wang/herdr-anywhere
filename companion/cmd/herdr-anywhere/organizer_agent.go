package main

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"time"

	"herdr-anywhere/companion/internal/hcbin"
	"herdr-anywhere/companion/internal/proc"
)

// Which agent does the filing: the user picks one when turning the progress
// page on. Only agents that report their status to herdr-core can be picked:
// the companion types a batch only into a filing pane that says it is free.
// Measured on live panes (hc ls/get, read only):
// claude-code, pi and kimi-code report (status.app); Codex reports from
// 0.163.0 on (its changelog).

type agentChoice struct {
	ID     string `json:"id"`
	Name   string `json:"name"`
	Cmd    string `json:"cmd"`
	Usable bool   `json:"usable"`
	Note   string `json:"note,omitempty"`
	// When it is too old: the version it needs and the one here, for the phone to say in its own language.
	Needs   string `json:"needs,omitempty"`
	Version string `json:"version,omitempty"`
	// Script: on Windows it is installed only as a .cmd script that is not npm's, or whose program is missing, so a
	// pane can't start the agent itself (scriptTarget); the phone says so in its own language.
	Script bool `json:"script,omitempty"`
}

type knownAgent struct {
	id, name, bin, cmd string
	minVersion         [3]int // zero: any version reports
}

// The model: Haiku 5.5 for Claude Code (its scores: companion/eval/filing);
// the others use the model their own settings name.
var knownAgents = []knownAgent{
	{id: "claude-code", name: "Claude Code", bin: "claude", cmd: "claude --model claude-haiku-5-5"},
	{id: "pi", name: "Pi", bin: "pi", cmd: "pi"},
	{id: "omp", name: "Oh My Pi", bin: "omp", cmd: "omp"},
	// Without --yolo every track command waits for a yes in the pane.
	{id: "kimi-code", name: "Kimi Code", bin: "kimi", cmd: "kimi --yolo"},
	// Codex: no update prompt (its default installs a new Codex for the whole computer), no asking before
	// commands, and its sandbox: it writes only in its folder and the progress file's (organizerPane).
	{id: "codex", name: "Codex", bin: "codex", cmd: "codex --no-daemon -c check_for_update_on_startup=false -a never -s workspace-write", minVersion: [3]int{0, 163, 0}},
}

// agentPaths finds the agents the way the user's own shell does: herdr-core
// runs the filing pane in an interactive shell, where installers add their
// folders (some installers, Kimi Code's for one, add their folder only to the
// shell's startup file).
var agentPaths = func() map[string]string {
	var script strings.Builder
	for _, a := range knownAgents {
		fmt.Fprintf(&script, "printf '%%s=%%s\\n' %s \"$(command -v %s)\";", a.bin, a.bin)
	}
	sh := os.Getenv("SHELL")
	if sh == "" {
		sh = "/bin/zsh"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	out, _ := proc.CommandContext(ctx, sh, "-ic", script.String()).Output()
	found := map[string]string{}
	for _, l := range strings.Split(string(out), "\n") {
		k, v, ok := strings.Cut(strings.TrimSpace(l), "=")
		if ok && strings.HasPrefix(v, "/") {
			found[k] = v
		}
	}
	for _, a := range knownAgents {
		if found[a.bin] == "" {
			if p, err := exec.LookPath(a.bin); err == nil {
				found[a.bin] = p
			}
		}
	}
	return found
}

// goos is runtime.GOOS; a test sets another.
var goos = runtime.GOOS

// agentVersion is what the agent's program says with --version: argv is the program the pane starts (agentTarget).
var agentVersion = func(argv []string) string {
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	out, _ := proc.CommandContext(ctx, argv[0], append(argv[1:len(argv):len(argv)], "--version")...).Output()
	return strings.TrimSpace(string(out))
}

// agentTarget is the program a pane starts for the agent found at path: on Windows an npm .cmd shim's own program
// (scriptTarget), found with the PATH given; false for a script whose program can't be found. Elsewhere path itself.
// Its version is asked of the same program.
func agentTarget(path, envPath string) ([]string, bool) {
	if goos == "windows" && windowsScript(path) {
		return scriptTarget(path, envPath)
	}
	return []string{path}, true
}

// panePath is the PATH a filing pane starts with: the companion's folder, then the companion's PATH (organizerPane).
func panePath() string {
	self, _ := os.Executable()
	return filepath.Dir(self) + string(os.PathListSeparator) + os.Getenv("PATH")
}

var versionRe = regexp.MustCompile(`(\d+)\.(\d+)\.(\d+)`)

func olderThan(v string, min [3]int) (string, bool) {
	m := versionRe.FindStringSubmatch(v)
	if m == nil {
		return v, true
	}
	for i := 0; i < 3; i++ {
		n, _ := strconv.Atoi(m[i+1])
		if n != min[i] {
			return m[0], n < min[i]
		}
	}
	return m[0], false
}

// organizerAgents is the agents on this computer, in the order they're offered.
func organizerAgents() []agentChoice {
	paths := agentPaths()
	out := []agentChoice{}
	for _, a := range knownAgents {
		p := paths[a.bin]
		if p == "" {
			continue
		}
		c := agentChoice{ID: a.id, Name: a.name, Cmd: a.cmd, Usable: true}
		argv, ok := agentTarget(p, panePath())
		if !ok {
			c.Usable, c.Script = false, true
			c.Note = "installed only as a .cmd script whose program can't be found, which herdr-core can't start as an agent: " + p
			out = append(out, c)
			continue
		}
		if a.minVersion != ([3]int{}) {
			if v, old := olderThan(agentVersion(argv), a.minVersion); old {
				c.Usable = false
				c.Needs, c.Version = fmt.Sprintf("%d.%d", a.minVersion[0], a.minVersion[1]), v
				c.Note = fmt.Sprintf("needs %s or later to report whether it is busy; this computer has %s", c.Needs, v)
			}
		}
		out = append(out, c)
	}
	return out
}

// pickOrganizer makes agent id the filing agent. A filing pane running
// another command is closed, so the next pass starts the new one.
func pickOrganizer(state, id, hcFlag, lang string) (organizerConfig, error) {
	cfg := loadOrganizer(state)
	if lang != "" && lang != cfg.Lang {
		cfg.Lang = lang
		if err := saveOrganizer(state, cfg); err != nil {
			return cfg, err
		}
	}
	var pick *agentChoice
	for _, a := range organizerAgents() {
		if a.ID == id {
			a := a
			pick = &a
		}
	}
	switch {
	case pick == nil:
		return cfg, fmt.Errorf("no agent %q on this computer (herdr-anywhere progress agents lists them)", id)
	case !pick.Usable:
		return cfg, fmt.Errorf("%s: %s", pick.Name, pick.Note)
	}
	if cfg.Cmd == pick.Cmd && cfg.Agent == pick.ID {
		return cfg, nil
	}
	changed := cfg.Cmd != pick.Cmd
	cfg.Cmd, cfg.Agent = pick.Cmd, pick.ID
	if err := saveOrganizer(state, cfg); err != nil {
		return cfg, err
	}
	if changed {
		home, _ := os.UserHomeDir()
		if hc, _, err := hcbin.Resolve(hcFlag, exec.LookPath, home); err == nil {
			if t, err := roleFind(hc, cfg.Name, organizerRole); err == nil && t != nil {
				if out, err := runFor(10*time.Second, hc, "kill", t.Address); err != nil {
					return cfg, fmt.Errorf("hc kill %s: %v %s", t.Address, err, out)
				}
			}
		}
	}
	return cfg, nil
}

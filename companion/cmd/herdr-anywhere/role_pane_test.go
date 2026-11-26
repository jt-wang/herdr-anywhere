package main

import (
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"herdr-anywhere/companion/internal/testfake"
)

// fakeAgentMark, in a filing pane's folder, makes this test binary started there the pane's agent.
const fakeAgentMark = "hm-fake-agent"

// The filing pane on a real herdr-core: its agent runs with this program's folder first on PATH and the companion's
// state folder, which the pane's rules rely on, and `hc kill` (progress off) ends it. On Windows too, which has no
// env program: the test hides Git's, as on a Windows without Git.
func TestRolePaneStartOnARealCore(t *testing.T) {
	if os.Getenv("HM_TEST_HC") == "" {
		t.Skip("needs herdr-core's hc in HM_TEST_HC")
	}
	if runtime.GOOS == "windows" {
		var keep []string
		for _, d := range filepath.SplitList(os.Getenv("PATH")) {
			if _, err := os.Stat(filepath.Join(d, "env.exe")); err != nil {
				keep = append(keep, d)
			}
		}
		t.Setenv("PATH", strings.Join(keep, string(os.PathListSeparator)))
	}
	hc := isolatedHC(t)
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(self, " ") {
		t.Fatalf("the test binary %q has a space in its path: start the test with TMPDIR on a path without one", self)
	}

	// The agent as the filing pane's command names it: a program by its path, and one the companion's PATH lacks,
	// found where `progress agents` found it (agentPaths), its folder then first on the pane's PATH.
	t.Run("path", func(t *testing.T) {
		paneOnACore(t, hc, self, nil, []string{filepath.Dir(self)})
	})
	t.Run("found by progress agents", func(t *testing.T) {
		bin := filepath.Join(testfake.ShortDir(t, "hmbin"), "hm-filing-agent"+filepath.Ext(self))
		copyFile(t, self, bin)
		defer func(f func() map[string]string) { agentPaths = f }(agentPaths)
		agentPaths = func() map[string]string { return map[string]string{"hm-filing-agent": bin} }
		paneOnACore(t, hc, "hm-filing-agent", nil, []string{filepath.Dir(bin), filepath.Dir(self)})
	})
	// On Windows, npm's .cmd shim of an agent that is a node script: node runs the script in the pane, not cmd.exe.
	t.Run("npm shim", func(t *testing.T) {
		if runtime.GOOS != "windows" {
			t.Skip("npm writes .cmd shims on Windows")
		}
		if _, err := exec.LookPath("node"); err != nil {
			t.Fatalf("needs node on PATH: %v", err)
		}
		npm := testfake.ShortDir(t, "hmnpm")
		js := filepath.Join(npm, "node_modules", "hm-fake", "agent.js")
		os.MkdirAll(filepath.Dir(js), 0o755)
		if err := os.WriteFile(js, []byte(fakeNodeAgent), 0o644); err != nil {
			t.Fatal(err)
		}
		shim := strings.ReplaceAll(npmScriptShim, `@earendil-works\pi-coding-agent\dist\bundle\cli.js`, `hm-fake\agent.js`)
		if err := os.WriteFile(filepath.Join(npm, "hm-fake.cmd"), []byte(shim), 0o755); err != nil {
			t.Fatal(err)
		}
		paneOnACore(t, hc, filepath.Join(npm, "hm-fake.cmd"), []string{}, []string{filepath.Dir(self)})
	})
	// Codex as npm installs it (HM_TEST_NPM_CODEX: the npm prefix), the way a user gets there: progress agents
	// offers it, it is picked, and its filing pane's program in front is Codex's own exe for node's architecture.
	// CI runs this built for x64 on Windows on Arm too, as the release's one Windows file runs there.
	t.Run("npm Codex", func(t *testing.T) {
		prefix := os.Getenv("HM_TEST_NPM_CODEX")
		if prefix == "" {
			t.Skip("needs Codex installed with npm in HM_TEST_NPM_CODEX")
		}
		state := filepath.Join(testfake.ShortDir(t, "hmstate"), "state")
		t.Setenv("HERDR_ANYWHERE_STATE", state)
		defer func(f func() map[string]string) { agentPaths = f }(agentPaths)
		agentPaths = func() map[string]string { return map[string]string{"codex": filepath.Join(prefix, "codex.cmd")} }
		var out strings.Builder
		if err := progressCommand([]string{"agents"}, &out, &out); err != nil {
			t.Fatal(err)
		}
		var listed struct{ Agents []agentChoice }
		if err := json.Unmarshal([]byte(out.String()), &listed); err != nil || len(listed.Agents) != 1 || !listed.Agents[0].Usable {
			t.Fatalf("progress agents: %v %s", err, out.String())
		}
		out.Reset()
		if err := progressCommand([]string{"organizer", "--agent", "codex", "--hc", hc}, &out, &out); err != nil {
			t.Fatalf("picking Codex: %v %s", err, out.String())
		}
		cfg := loadOrganizer(state)
		addr, err := rolePaneStart(hc, organizerPane(cfg, state), organizerRole, organizerRulesText(), organizerSettings, "HERDR_ANYWHERE_STATE="+state)
		if err != nil {
			t.Fatalf("the filing pane did not start: %v", err)
		}
		t.Cleanup(func() { exec.Command(hc, "kill", addr).Run(); exec.Command(hc, "rm", addr).Run() })
		node, err := exec.LookPath("node")
		if err != nil {
			t.Fatal(err)
		}
		arch := map[uint16]string{0x8664: "x64", 0xAA64: "arm64"}[peMachine(node)]
		var g struct {
			State      string `json:"state"`
			Foreground *struct {
				Name string   `json:"name"`
				Argv []string `json:"argv"`
			} `json:"foreground"`
		}
		for deadline := time.Now().Add(30 * time.Second); ; time.Sleep(200 * time.Millisecond) {
			b, err := exec.Command(hc, "get", addr, "--json").Output()
			if err == nil && json.Unmarshal(b, &g) == nil && g.Foreground != nil && len(g.Foreground.Argv) > 0 &&
				strings.EqualFold(g.Foreground.Name, "codex.exe") &&
				strings.Contains(strings.ToLower(g.Foreground.Argv[0]), `\codex-win32-`+arch+`\`) {
				break
			}
			if time.Now().After(deadline) {
				screen, _ := exec.Command(hc, "read", addr).CombinedOutput()
				t.Fatalf("herdr-core's program in front of the filing pane: %s; want Codex's codex-win32-%s exe (this test built for %s)\n%s",
					b, arch, runtime.GOARCH, screen)
			}
		}
		t.Logf("in front: %s (node %s, this test built for %s)", g.Foreground.Argv[0], arch, runtime.GOARCH)
	})
}

// fakeNodeAgent is TestFakeFilingAgent as a node script, an agent npm installs.
const fakeNodeAgent = `const fs = require('fs'), path = require('path')
fs.writeFileSync('agent.json', JSON.stringify({ pid: process.pid, state: process.env.HERDR_ANYWHERE_STATE || '',
  herdr: process.env.HERDR_TRACK_HERDR || '', path: (process.env.PATH || '').split(path.delimiter).slice(0, 3) }))
setInterval(() => {}, 1000)
`

func copyFile(t *testing.T, from, to string) {
	b, err := os.ReadFile(from)
	if err == nil {
		err = os.WriteFile(to, b, 0o755)
	}
	if err != nil {
		t.Fatal(err)
	}
}

// paneOnACore starts a filing pane whose command is cmd (with extra after it; nil: this test binary's
// TestFakeFilingAgent), and checks its agent: its state folder, HERDR_TRACK_HERDR and PATH's first folders
// (wantPath), herdr-core's program in front, and its end on hc kill.
func paneOnACore(t *testing.T, hc, cmd string, extra, wantPath []string) {
	dir := testfake.ShortDir(t, "hmpane")
	if extra == nil {
		extra = []string{"-test.run=^TestFakeFilingAgent$"}
		if err := os.WriteFile(filepath.Join(dir, fakeAgentMark), nil, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	state := filepath.Join(testfake.ShortDir(t, "hmstate"), "state")
	pane := rolePane{Name: "filing-test", Dir: dir, Cmd: cmd, Extra: extra}
	addr, err := rolePaneStart(hc, pane, "filing-test", "rules", "", "HERDR_ANYWHERE_STATE="+state, "HERDR_TRACK_HERDR=-")
	if err != nil {
		t.Fatalf("the filing pane did not start: %v", err)
	}
	t.Cleanup(func() { exec.Command(hc, "kill", addr).Run(); exec.Command(hc, "rm", addr).Run() })

	var got fakeAgentSaw
	deadline := time.Now().Add(30 * time.Second)
	for {
		b, err := os.ReadFile(filepath.Join(dir, "agent.json"))
		if err == nil && json.Unmarshal(b, &got) == nil {
			break
		}
		if time.Now().After(deadline) {
			screen, _ := exec.Command(hc, "read", addr).CombinedOutput()
			t.Fatalf("the agent never started in %s:\n%s", addr, screen)
		}
		time.Sleep(200 * time.Millisecond)
	}
	if got.State != state || got.Herdr != "-" || len(got.Path) < len(wantPath) || fmt.Sprint(got.Path[:len(wantPath)]) != fmt.Sprint(wantPath) {
		t.Fatalf("the agent ran with state %q, HERDR_TRACK_HERDR %q, PATH %q; want %q, %q, PATH starting %q",
			got.State, got.Herdr, got.Path, state, "-", wantPath)
	}

	// herdr-core reads the agent's status from the program in front, which must be the agent itself.
	var g struct {
		Foreground *struct {
			Pid int `json:"pid"`
		} `json:"foreground"`
	}
	for {
		out, err := exec.Command(hc, "get", addr, "--json").Output()
		if err == nil && json.Unmarshal(out, &g) == nil && g.Foreground != nil && g.Foreground.Pid == got.Pid {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("herdr-core's program in front of %s: %s, want the agent, pid %d", addr, out, got.Pid)
		}
		time.Sleep(200 * time.Millisecond)
	}

	if out, err := exec.Command(hc, "kill", addr).CombinedOutput(); err != nil {
		t.Fatalf("hc kill: %v %s", err, out)
	}
	for alive(got.Pid) {
		if time.Now().After(deadline) {
			t.Fatalf("the agent, pid %d, still runs after hc kill", got.Pid)
		}
		time.Sleep(200 * time.Millisecond)
	}
}

type fakeAgentSaw struct {
	Pid   int      `json:"pid"`
	State string   `json:"state"`
	Herdr string   `json:"herdr"`
	Path  []string `json:"path"` // its first folders
}

// TestFakeFilingAgent is the filing pane's agent in TestRolePaneStartOnARealCore: it writes what it was started
// with into its folder and waits to be ended.
func TestFakeFilingAgent(t *testing.T) {
	if _, err := os.Stat(fakeAgentMark); err != nil {
		t.Skip("only as the agent of TestRolePaneStartOnARealCore's filing pane")
	}
	b, _ := json.Marshal(fakeAgentSaw{Pid: os.Getpid(), State: os.Getenv("HERDR_ANYWHERE_STATE"),
		Herdr: os.Getenv("HERDR_TRACK_HERDR"), Path: filepath.SplitList(os.Getenv("PATH"))[:3]})
	if err := os.WriteFile("agent.json", b, 0o600); err != nil {
		t.Fatal(err)
	}
	time.Sleep(10 * time.Minute)
}

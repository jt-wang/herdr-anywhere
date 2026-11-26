package main

import (
	"encoding/json"
	"fmt"
	"herdr-anywhere/companion/internal/testfake"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// Turning the progress page on, the user picks which agent does the filing.
// Offered: the agents on this computer that report their status to herdr-core,
// which the companion needs to know the filing pane is free. Measured on
// live panes (hc ls/get, read only): claude-code,
// pi and kimi-code report; Codex reports only from 0.163.
func TestTheFilingAgentIsPickedFromTheOnesThatReportTheirStatus(t *testing.T) {
	state := t.TempDir()
	t.Setenv("HERDR_ANYWHERE_STATE", state)
	defer func(f func() map[string]string, v func([]string) string) { agentPaths, agentVersion = f, v }(agentPaths, agentVersion)
	agentPaths = func() map[string]string {
		return map[string]string{"claude": "/x/claude", "pi": "/x/pi", "codex": "/x/codex"} // no kimi
	}
	agentVersion = func([]string) string { return "codex-cli 0.150.0" }
	var out strings.Builder
	if err := progressCommand([]string{"agents"}, &out, &out); err != nil {
		t.Fatal(err)
	}
	var got struct {
		Agents []agentChoice `json:"agents"`
		Chosen string        `json:"chosen"`
	}
	if err := json.Unmarshal([]byte(out.String()), &got); err != nil {
		t.Fatalf("%v %s", err, out.String())
	}
	var ids []string
	for _, a := range got.Agents {
		ids = append(ids, a.ID+":"+map[bool]string{true: "ok", false: "no"}[a.Usable])
	}
	if strings.Join(ids, " ") != "claude-code:ok pi:ok codex:no" || got.Chosen != "" {
		t.Fatalf("agents = %v chosen %q", ids, got.Chosen)
	}
	if c := got.Agents[2]; !strings.Contains(c.Note, "0.163") || !strings.Contains(c.Note, "0.150.0") {
		t.Fatalf("codex note = %q", c.Note)
	}
	// The phone writes the note in its own language from these two.
	if c := got.Agents[2]; c.Needs != "0.163" || c.Version != "0.150.0" {
		t.Fatalf("codex needs %q, has %q", c.Needs, c.Version)
	}
	if c := got.Agents[0]; c.Name != "Claude Code" || c.Cmd != "claude --model claude-haiku-5-5" {
		t.Fatalf("claude = %+v", c)
	}

	// Picking one: its command runs the filing pane from then on, and a filing
	// pane already running another agent is closed, so the next pass starts the new one.
	d := t.TempDir()
	hc := testfake.HC(t, d, "hc", testfake.Spec{Version: "hc 0.1.18", On: map[string][]testfake.Reply{
		"ls":   {{Print: `{"terminals":[{"address":"t8","name":"herdr-organizer","state":"live","status":"idle","turn":1,"agent":"claude-code"}]}`}},
		"get":  {{Print: `{"address":"t8","state":"live","labels":{"herdr.role":"organizer"},"status":{"state":"idle"}}`}},
		"kill": {{Append: filepath.Join(d, "killed")}},
	}})
	out.Reset()
	if err := progressCommand([]string{"organizer", "--agent", "pi", "--hc", hc}, &out, &out); err != nil {
		t.Fatalf("%v %s", err, out.String())
	}
	if cfg := loadOrganizer(state); cfg.Cmd != "pi" || cfg.Agent != "pi" {
		t.Fatalf("cfg = %+v", cfg)
	}
	if b, _ := os.ReadFile(filepath.Join(d, "killed")); strings.TrimSpace(string(b)) != "kill t8" {
		t.Fatalf("killed: %q", b)
	}
	// The same pick again leaves the pane alone.
	os.Remove(filepath.Join(d, "killed"))
	progressCommand([]string{"organizer", "--agent", "pi", "--hc", hc}, &out, &out)
	if _, err := os.Stat(filepath.Join(d, "killed")); err == nil {
		t.Fatal("killed the pane for the same pick")
	}
	for _, bad := range []string{"codex", "kimi-code", "nope"} {
		if err := progressCommand([]string{"organizer", "--agent", bad, "--hc", hc}, &out, &out); err == nil {
			t.Fatalf("picked %s", bad)
		}
	}
	out.Reset()
	progressCommand([]string{"agents"}, &out, &out)
	if !strings.Contains(out.String(), `"chosen": "pi"`) {
		t.Fatalf("chosen not shown: %s", out.String())
	}
}

// Codex as the filing agent (measured in companion/eval/filing): without these it
// stops at an update prompt whose default installs a new Codex for the whole
// computer, and asks before each track command. It may write only in the filing
// folder and the progress file's folder, a path that may hold spaces.
func TestCodexFilesWithoutPromptsAndWritesOnlyWhereItMust(t *testing.T) {
	var cmd string
	for _, a := range knownAgents {
		if a.id == "codex" {
			cmd = a.cmd
		}
	}
	for _, want := range []string{"--no-daemon", "check_for_update_on_startup=false", "-a never", "-s workspace-write"} {
		if !strings.Contains(cmd, want) {
			t.Errorf("codex command %q lacks %s", cmd, want)
		}
	}
	state := filepath.Join(t.TempDir(), "with space")
	argv, err := organizerPane(organizerConfig{Agent: "codex", Cmd: cmd, Dir: "/x"}, state).argv()
	if err != nil {
		t.Fatal(err)
	}
	if got := strings.Join(argv[len(argv)-2:], "|"); got != "--add-dir|"+state {
		t.Errorf("codex argv ends %q", got)
	}
	argv, _ = organizerPane(organizerConfig{Agent: "pi", Cmd: "pi", Dir: "/x"}, state).argv()
	if strings.Join(argv, " ") != "pi" {
		t.Errorf("pi argv %q", argv)
	}
}

// On Windows npm installs every agent's command as a .cmd shim. One whose program is there is offered (the pane
// starts that program, TestAnNpmShimIsStartedAsWhatItRuns); a script that is not npm's is listed, not usable, and
// can't be picked: run through cmd.exe, the agent would not be the program in front. Elsewhere a file named so is
// the program itself.
func TestAWindowsScriptIsOfferedOnlyWhenItsProgramIsFound(t *testing.T) {
	t.Setenv("HERDR_ANYWHERE_STATE", t.TempDir())
	npm := t.TempDir()
	codexExe := "node_modules/@openai/codex/node_modules/@openai/codex-win32-arm64/vendor/aarch64-pc-windows-msvc/bin/codex.exe"
	for rel, text := range map[string]string{"pi.cmd": npmScriptShim, "node.exe": fakePE(0xAA64),
		"node_modules/@earendil-works/pi-coding-agent/dist/bundle/cli.js": "", "kimi.bat": "@echo off\r\npython -m kimi %*\r\n",
		"codex.cmd": strings.ReplaceAll(npmScriptShim, `@earendil-works\pi-coding-agent\dist\bundle\cli.js`, `@openai\codex\bin\codex.js`),
		"node_modules/@openai/codex/bin/codex.js": "", codexExe: "",
		"node_modules/@openai/codex/node_modules/@openai/codex-win32-arm64/package.json": "{}"} {
		p := filepath.Join(npm, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	defer func(f func() map[string]string, g string, v func([]string) string) {
		agentPaths, goos, agentVersion = f, g, v
	}(agentPaths, goos, agentVersion)
	agentPaths = func() map[string]string {
		return map[string]string{"claude": `C:\x\claude.exe`, "pi": filepath.Join(npm, "pi.cmd"), "kimi": filepath.Join(npm, "kimi.bat"),
			"codex": filepath.Join(npm, "codex.cmd")}
	}
	// Codex's version is asked of the program the pane starts, never of its .cmd.
	var asked []string
	agentVersion = func(argv []string) string { asked = argv; return "codex-cli 0.170.0" }
	for _, c := range []struct{ goos, want string }{{"windows", "claude-code:ok pi:ok kimi-code:script codex:ok"}, {"linux", "claude-code:ok pi:ok kimi-code:ok codex:ok"}} {
		goos, asked = c.goos, nil
		var ids []string
		for _, a := range organizerAgents() {
			s := map[bool]string{true: "ok", false: "no"}[a.Usable]
			if a.Script && !a.Usable {
				s = "script"
			}
			ids = append(ids, a.ID+":"+s)
		}
		if strings.Join(ids, " ") != c.want {
			t.Fatalf("%s: agents = %v, want %s", c.goos, ids, c.want)
		}
		want := []string{filepath.Join(npm, filepath.FromSlash(codexExe))}
		if c.goos != "windows" {
			want = []string{filepath.Join(npm, "codex.cmd")}
		}
		if fmt.Sprint(asked) != fmt.Sprint(want) {
			t.Fatalf("%s: Codex's version asked of %q, want %q", c.goos, asked, want)
		}
	}
	goos = "windows"
	if _, err := pickOrganizer(os.Getenv("HERDR_ANYWHERE_STATE"), "kimi-code", "", "en"); err == nil || !strings.Contains(err.Error(), ".cmd") {
		t.Fatalf("picking Kimi Code installed as a script: %v", err)
	}
}

// npm's .cmd shim (cmd-shim), as `npm install -g` writes it for a package whose command is a script: Pi 1.6.1's,
// copied from a Windows runner.
const npmScriptShim = "@ECHO off\r\nGOTO start\r\n:find_dp0\r\nSET dp0=%~dp0\r\nEXIT /b\r\n:start\r\nSETLOCAL\r\nCALL :find_dp0\r\n\r\n" +
	"IF EXIST \"%dp0%\\node.exe\" (\r\n  SET \"_prog=%dp0%\\node.exe\"\r\n) ELSE (\r\n  SET \"_prog=node\"\r\n  SET PATHEXT=%PATHEXT:;.JS;=;%\r\n)\r\n\r\n" +
	"endLocal & goto #_undefined_# 2>NUL || title %COMSPEC% & \"%_prog%\"  \"%dp0%\\node_modules\\@earendil-works\\pi-coding-agent\\dist\\bundle\\cli.js\" %*\r\n"

// ... and for a package whose command is a program of its own.
const npmExeShim = "@ECHO off\r\nGOTO start\r\n:find_dp0\r\nSET dp0=%~dp0\r\nEXIT /b\r\n:start\r\nSETLOCAL\r\nCALL :find_dp0\r\n" +
	"\"%dp0%\\node_modules\\@anthropic-ai\\claude-code\\bin\\claude.exe\"   %*\r\n"

// An npm shim runs through cmd.exe; the pane runs what the shim runs instead, so the agent is the program in front:
// node with the package's script (node beside the shim, else on PATH), the package's .exe, or Codex's own exe. A script
// that is not npm's, or whose program is missing, can't be started so.
func TestAnNpmShimIsStartedAsWhatItRuns(t *testing.T) {
	dir := t.TempDir()
	put := func(rel, text string) string {
		p := filepath.Join(dir, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cli := put("node_modules/@earendil-works/pi-coding-agent/dist/bundle/cli.js", "")
	exe := put("node_modules/@anthropic-ai/claude-code/bin/claude.exe", "")
	pi, claude := put("pi.cmd", npmScriptShim), put("claude.cmd", npmExeShim)
	other := put("tool.cmd", "@echo off\r\npython -m tool %*\r\n")
	gone := put("gone/codex.cmd", strings.ReplaceAll(npmScriptShim, "pi-coding-agent", "codex"))
	// node on PATH: node on macOS and Linux, node.exe on Windows (exec.LookPath).
	nodeOnPath := t.TempDir()
	for _, n := range []string{"node", "node.exe"} {
		if err := os.WriteFile(filepath.Join(nodeOnPath, n), nil, 0o755); err != nil {
			t.Fatal(err)
		}
	}

	if got, ok := scriptTarget(claude, ""); !ok || fmt.Sprint(got) != fmt.Sprint([]string{exe}) {
		t.Fatalf("claude.cmd: %q %v, want %q", got, ok, exe)
	}
	if _, ok := scriptTarget(pi, ""); ok {
		t.Fatalf("pi.cmd with no node anywhere: started")
	}
	if got, ok := scriptTarget(pi, nodeOnPath); !ok || len(got) != 2 || filepath.Dir(got[0]) != nodeOnPath || got[1] != cli {
		t.Fatalf("pi.cmd, node on PATH: %q %v, want [%s/node… %s]", got, ok, nodeOnPath, cli)
	}
	beside := put("node.exe", "")
	if got, ok := scriptTarget(pi, nodeOnPath); !ok || fmt.Sprint(got) != fmt.Sprint([]string{beside, cli}) {
		t.Fatalf("pi.cmd, node beside it: %q %v, want %q", got, ok, []string{beside, cli})
	}
	for _, p := range []string{other, gone} {
		if got, ok := scriptTarget(p, nodeOnPath); ok {
			t.Fatalf("%s: started as %q", p, got)
		}
	}

	// Codex's script runs Codex's own exe as its child, so node would be in front: the pane starts that exe, the one
	// codex.js picks for node's architecture (not the Companion's: an x64 Companion runs on Arm Windows), from the
	// platform package Node's require finds, nested where npm put it on a Windows runner or hoisted.
	cx := t.TempDir()
	cput := func(rel, text string) string {
		p := filepath.Join(cx, filepath.FromSlash(rel))
		os.MkdirAll(filepath.Dir(p), 0o755)
		if err := os.WriteFile(p, []byte(text), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	cput("node_modules/@openai/codex/bin/codex.js", "")
	codex := cput("codex.cmd", strings.ReplaceAll(npmScriptShim, `@earendil-works\pi-coding-agent\dist\bundle\cli.js`, `@openai\codex\bin\codex.js`))
	nested := "node_modules/@openai/codex/node_modules/@openai/"
	x64 := cput(nested+"codex-win32-x64/vendor/x86_64-pc-windows-msvc/bin/codex.exe", "")
	cput(nested+"codex-win32-x64/package.json", "{}")
	arm := cput(nested+"codex-win32-arm64/vendor/aarch64-pc-windows-msvc/bin/codex.exe", "")
	armPkg := cput(nested+"codex-win32-arm64/package.json", "{}")
	own := cput("node_modules/@openai/codex/vendor/aarch64-pc-windows-msvc/bin/codex.exe", "")
	want := func(what string, w ...string) {
		t.Helper()
		got, ok := scriptTarget(codex, "")
		if fmt.Sprint(got) != fmt.Sprint(w) || ok != (len(w) > 0) {
			t.Fatalf("codex.cmd, %s: %q %v, want %q", what, got, ok, w)
		}
	}
	want("no node")
	cput("node.exe", "")
	want("node.exe that is not a program")
	cput("node.exe", fakePE(0xAA64))
	want("Arm64 node", arm)
	cput("node.exe", fakePE(0x8664))
	want("x64 node", x64)
	cput("node.exe", fakePE(0xAA64))
	os.Remove(arm)
	want("Arm64 node, its platform package without its exe")
	os.Remove(armPkg)
	want("Arm64 node, no platform package: Codex's own vendor", own)
	os.Remove(own)
	want("Arm64 node, nothing to run")
	hoisted := cput("node_modules/@openai/codex-win32-arm64/vendor/aarch64-pc-windows-msvc/bin/codex.exe", "")
	cput("node_modules/@openai/codex-win32-arm64/package.json", "{}")
	want("Arm64 node, platform package hoisted", hoisted)
}

// fakePE is the start of a Windows program built for machine (peMachine reads no further).
func fakePE(machine uint16) string {
	b := make([]byte, 0x46)
	b[0], b[1], b[0x3c] = 'M', 'Z', 0x40
	copy(b[0x40:], "PE\x00\x00")
	b[0x44], b[0x45] = byte(machine), byte(machine>>8)
	return string(b)
}

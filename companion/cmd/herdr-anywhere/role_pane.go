// Herdr's own panes in herdr-core (the filing pane): the label that marks them, how
// one is started, and the fields of `hc ls` it is found by.
package main

import (
	"bufio"
	"context"
	"encoding/binary"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"herdr-anywhere/companion/internal/proc"
)

// roleLabel marks a pane Herdr started for itself; its value is the pane's role.
const roleLabel = "herdr.role"

// rolePane is how a helper pane is started: its name, folder and command.
type rolePane struct {
	Name  string
	Dir   string
	Cmd   string   // an argv line, split on spaces
	Extra []string // arguments after the command, as they are (a path may hold spaces)
}

func (c rolePane) argv() ([]string, error) {
	if strings.TrimSpace(c.Cmd) == "" {
		return nil, fmt.Errorf("pane %s has no command", c.Name)
	}
	return append(strings.Fields(c.Cmd), c.Extra...), nil
}

type hcLsTerm struct {
	Address string `json:"address"`
	Name    string `json:"name"`
	State   string `json:"state"`
	Status  string `json:"status"`
}

// hcStart starts a terminal through herdr-core's API (terminal.run over `hc connect`), and gives its address.
// Unlike `hc run`, the API takes the program's environment; Windows has no env program to put in front of it.
func hcStart(hc string, params map[string]any) (string, error) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	cmd := proc.CommandContext(ctx, hc, "connect")
	in, err := cmd.StdinPipe()
	if err != nil {
		return "", err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return "", err
	}
	if err := cmd.Start(); err != nil {
		return "", fmt.Errorf("hc connect: %w", err)
	}
	defer func() { in.Close(); cmd.Wait() }()
	req, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": "terminal.run", "params": params})
	if _, err := in.Write(append(req, '\n')); err != nil {
		return "", fmt.Errorf("hc connect: %w", err)
	}
	line, err := bufio.NewReader(out).ReadBytes('\n')
	if err != nil {
		return "", fmt.Errorf("hc connect: no answer: %w", err)
	}
	var r struct {
		Result *struct {
			Address string `json:"address"`
		} `json:"result"`
		Error *struct {
			Message string `json:"message"`
			Data    struct {
				Kind string `json:"kind"`
			} `json:"data"`
		} `json:"error"`
	}
	switch {
	case json.Unmarshal(line, &r) != nil:
		return "", fmt.Errorf("terminal.run: unexpected answer %q", strings.TrimSpace(string(line)))
	case r.Error != nil:
		return "", fmt.Errorf("terminal.run: %s (%s)", r.Error.Message, r.Error.Data.Kind)
	case r.Result == nil || r.Result.Address == "":
		return "", fmt.Errorf("terminal.run: unexpected answer %q", strings.TrimSpace(string(line)))
	}
	return r.Result.Address, nil
}

// lookPathIn is exec.LookPath on the PATH given.
func lookPathIn(name, path string) (string, error) {
	if strings.ContainsAny(name, `/\`) {
		return exec.LookPath(name)
	}
	for _, d := range filepath.SplitList(path) {
		if d == "" {
			continue
		}
		if p, err := exec.LookPath(filepath.Join(d, name)); err == nil {
			return p, nil
		}
	}
	return "", exec.ErrNotFound
}

// windowsScript: a .cmd or .bat file, which Windows runs only through cmd.exe: started in a pane so, cmd.exe would
// be its program in front, not the agent, and herdr-core could not tell the agent's status. npm installs every
// agent's command so (scriptTarget).
func windowsScript(path string) bool {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".cmd", ".bat":
		return true
	}
	return false
}

// npmShim is the line of an npm .cmd shim (npm's cmd-shim) that runs the package's own program: its path from the
// shim's folder, then the shim's arguments.
var npmShim = regexp.MustCompile(`"%~?dp0%?\\([^"]+)"\s+%\*`)

// scriptTarget is what an npm .cmd shim runs, to start it without cmd.exe: the package's .exe itself, Codex's own exe
// for Codex's script (codexScript), or node with the package's script, node being the one beside the shim or else on
// the PATH given, as the shim finds it. false: not such a shim, or what it runs is not there.
func scriptTarget(shim, path string) ([]string, bool) {
	b, err := os.ReadFile(shim)
	if err != nil {
		return nil, false
	}
	m := npmShim.FindAllSubmatch(b, -1)
	if m == nil {
		return nil, false
	}
	dir := filepath.Dir(shim)
	target := filepath.Join(dir, filepath.FromSlash(strings.ReplaceAll(string(m[len(m)-1][1]), `\`, "/")))
	if _, err := os.Stat(target); err != nil {
		return nil, false
	}
	if strings.EqualFold(filepath.Ext(target), ".exe") {
		return []string{target}, true
	}
	node := filepath.Join(dir, "node.exe")
	if _, err := os.Stat(node); err != nil {
		if node, err = lookPathIn("node", path); err != nil {
			return nil, false
		}
	}
	if codexScript(target) {
		if exe := codexExe(target, node); exe != "" {
			return []string{exe}, true
		}
		return nil, false
	}
	return []string{node, target}, true
}

// codexScript: Codex's npm script, @openai/codex/bin/codex.js. On Windows it runs Codex's own exe as its child, so
// started with node it leaves node in front (measured on a Windows runner); the pane starts that exe (codexExe).
func codexScript(script string) bool {
	pkg := filepath.Dir(filepath.Dir(script))
	return strings.EqualFold(filepath.Base(script), "codex.js") && strings.EqualFold(filepath.Base(pkg), "codex") &&
		strings.EqualFold(filepath.Base(filepath.Dir(pkg)), "@openai")
}

// codexExe is the exe codex.js runs when node runs it, found as codex.js finds it (codex-cli/bin/codex.js): the
// platform package for node's own architecture (process.arch, here read from node.exe), @openai/codex-win32-x64 or
// -arm64, where Node's require.resolve finds its package.json from the script's folder up, and in it
// vendor/<target>/bin/codex.exe; without that package, the same path in Codex's own package. The Companion's own
// architecture plays no part: an x64 Companion runs on Arm Windows. "": not there, so codex.js would fail too.
func codexExe(script, node string) string {
	var pkg, target string
	switch peMachine(node) {
	case 0x8664:
		pkg, target = "codex-win32-x64", "x86_64-pc-windows-msvc"
	case 0xAA64:
		pkg, target = "codex-win32-arm64", "aarch64-pc-windows-msvc"
	default:
		return ""
	}
	exe := filepath.Join("vendor", target, "bin", "codex.exe")
	vendor := filepath.Join(filepath.Dir(filepath.Dir(script)), exe)
	for d := filepath.Dir(script); ; d = filepath.Dir(d) {
		// Node looks in <folder>/node_modules for each folder up, skipping folders that are node_modules themselves.
		if !strings.EqualFold(filepath.Base(d), "node_modules") {
			p := filepath.Join(d, "node_modules", "@openai", pkg)
			if _, err := os.Stat(filepath.Join(p, "package.json")); err == nil {
				vendor = filepath.Join(p, exe)
				break
			}
		}
		if filepath.Dir(d) == d {
			break
		}
	}
	if _, err := os.Stat(vendor); err != nil {
		return ""
	}
	return vendor
}

// peMachine is the machine a Windows executable is built for (the COFF header's Machine: 0x8664 x64, 0xAA64 Arm64),
// 0 when it is not one.
func peMachine(path string) uint16 {
	f, err := os.Open(path)
	if err != nil {
		return 0
	}
	defer f.Close()
	var h [0x40]byte
	if _, err := io.ReadFull(f, h[:]); err != nil || h[0] != 'M' || h[1] != 'Z' {
		return 0
	}
	var pe [6]byte
	if _, err := f.ReadAt(pe[:], int64(binary.LittleEndian.Uint32(h[0x3c:]))); err != nil || string(pe[:4]) != "PE\x00\x00" {
		return 0
	}
	return binary.LittleEndian.Uint16(pe[4:])
}

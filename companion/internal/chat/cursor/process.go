package cursor

import (
	"context"
	"encoding/json"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"time"

	"herdr-anywhere/companion/internal/chat"
)

type process struct {
	parent int
	argv   []string
}
type inspect func(int) (process, bool)

func processInfo(pid int) (process, bool) {
	if pid <= 1 {
		return process{}, false
	}
	if runtime.GOOS == "linux" {
		base := filepath.Join("/proc", strconv.Itoa(pid))
		status, e := os.ReadFile(filepath.Join(base, "status"))
		if e != nil {
			return process{}, false
		}
		p := process{}
		for _, line := range strings.Split(string(status), "\n") {
			if strings.HasPrefix(line, "State:") && strings.Contains(line, "Z (zombie)") {
				return process{}, false
			}
			if strings.HasPrefix(line, "PPid:") {
				p.parent, _ = strconv.Atoi(strings.TrimSpace(strings.TrimPrefix(line, "PPid:")))
			}
		}
		argv, e := os.ReadFile(filepath.Join(base, "cmdline"))
		if e != nil || len(argv) == 0 {
			return process{}, false
		}
		p.argv = strings.Split(strings.TrimRight(string(argv), "\x00"), "\x00")
		return p, true
	}
	// ps is also how the other readers inspect processes on macOS. Unsupported OSes fail closed.
	if runtime.GOOS != "darwin" {
		return process{}, false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	b, e := exec.CommandContext(ctx, "ps", "-p", strconv.Itoa(pid), "-o", "ppid=", "-o", "command=").Output()
	if e != nil {
		return process{}, false
	}
	fields := strings.Fields(string(b))
	if len(fields) < 2 {
		return process{}, false
	}
	parent, e := strconv.Atoi(fields[0])
	return process{parent, fields[1:]}, e == nil
}

func cursorCommand(argv []string) bool {
	if len(argv) < 2 {
		return false
	}
	name := strings.TrimSuffix(filepath.Base(argv[0]), ".exe")
	if name != "cursor-agent" && name != "agent" {
		return false
	}
	for _, arg := range argv[1:] {
		if filepath.Base(arg) == "index.js" {
			return true
		}
	}
	return false
}

func ancestor(pid int, get inspect) int {
	seen := map[int]bool{}
	for i := 0; pid > 1 && i < 64 && !seen[pid]; i++ {
		seen[pid] = true
		p, ok := get(pid)
		if !ok {
			return 0
		}
		if cursorCommand(p.argv) {
			return pid
		}
		pid = p.parent
	}
	return 0
}

func validProcess(pid, terminal int, get inspect) bool {
	p, ok := get(pid)
	if terminal <= 1 || !ok || !cursorCommand(p.argv) {
		return false
	}
	seen := map[int]bool{}
	for i := 0; pid > 1 && i < 64 && !seen[pid]; i++ {
		if pid == terminal {
			return true
		}
		seen[pid] = true
		p, ok = get(pid)
		if !ok {
			return false
		}
		pid = p.parent
	}
	return false
}

func processTree() map[int]process {
	tree := map[int]process{}
	if runtime.GOOS == "linux" {
		entries, _ := os.ReadDir("/proc")
		for _, entry := range entries {
			if pid, err := strconv.Atoi(entry.Name()); err == nil {
				if p, ok := processInfo(pid); ok {
					tree[pid] = p
				}
			}
		}
	} else if runtime.GOOS == "darwin" {
		ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
		defer cancel()
		out, err := exec.CommandContext(ctx, "ps", "-axo", "pid=,ppid=,command=").Output()
		if err != nil {
			return tree
		}
		for _, line := range strings.Split(string(out), "\n") {
			fields := strings.Fields(line)
			if len(fields) < 3 {
				continue
			}
			pid, e1 := strconv.Atoi(fields[0])
			parent, e2 := strconv.Atoi(fields[1])
			if e1 == nil && e2 == nil {
				tree[pid] = process{parent, fields[2:]}
			}
		}
	}
	return tree
}

func unlabeledCursor(terminal int, tree map[int]process) int {
	get := func(pid int) (process, bool) { p, ok := tree[pid]; return p, ok }
	found := 0
	for pid := range tree {
		if validProcess(pid, terminal, get) {
			if found != 0 {
				return 0
			}
			found = pid
		}
	}
	return found
}

// UnreportedPane is used only without any reported app or foreground. Labels never select another agent's reader.
func (r *Reader) UnreportedPane(terminal int, labels map[string]string) (chat.Pane, bool) {
	return unreportedPane(terminal, labels, processInfo, processTree)
}

func unreportedPane(terminal int, labels map[string]string, get inspect, tree func() map[int]process) (chat.Pane, bool) {
	if !supported() || terminal <= 1 {
		return chat.Pane{}, false
	}
	pid, e := strconv.Atoi(labels[chat.LabelPid])
	if e != nil || !validProcess(pid, terminal, get) {
		labels = nil
		pid = terminal
		// Before hooks exist, a shell-launched Cursor can offer setup, never a guessed conversation.
		if !validProcess(pid, terminal, get) {
			pid = unlabeledCursor(terminal, tree())
			if pid == 0 || !validProcess(pid, terminal, get) {
				return chat.Pane{}, false
			}
		}
	}
	return chat.Pane{Pid: pid, Cwd: labels[LabelCwd], Labels: labels}, true
}

// Hook deliberately has no output and no failure exit status: a failed label must not block a Cursor prompt.
func Hook(input io.Reader, hc string) {
	if !supported() {
		return
	}
	var p struct {
		Conversation string   `json:"conversation_id"`
		Roots        []string `json:"workspace_roots"`
	}
	b, e := io.ReadAll(io.LimitReader(input, 64<<10))
	if e != nil || json.Unmarshal(b, &p) != nil || !conversationID.MatchString(p.Conversation) || len(p.Roots) == 0 {
		return
	}
	cwd := p.Roots[0]
	if !filepath.IsAbs(cwd) || filepath.Clean(cwd) != cwd {
		return
	}
	terminal := os.Getenv("HC_TERMINAL_ID")
	if !regexpTerminal(terminal) {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 700*time.Millisecond)
	defer cancel()
	pid := ancestor(os.Getppid(), func(pid int) (process, bool) {
		if ctx.Err() != nil {
			return process{}, false
		}
		return processInfo(pid)
	})
	if pid == 0 {
		return
	}
	if !filepath.IsAbs(hc) {
		return
	}
	cmd := exec.CommandContext(ctx, hc, "label", terminal, chat.LabelPid+"="+strconv.Itoa(pid), chat.LabelSession+"="+p.Conversation, LabelCwd+"="+cwd)
	cmd.Stdout = io.Discard
	cmd.Stderr = io.Discard
	_ = cmd.Run()
}

func regexpTerminal(s string) bool {
	if len(s) < 2 || s[0] != 't' {
		return false
	}
	for _, c := range s[1:] {
		if !(c >= '0' && c <= '9' || c >= 'a' && c <= 'z') {
			return false
		}
	}
	return true
}

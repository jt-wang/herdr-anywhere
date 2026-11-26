package main

// TestLiveNoConsoleWindow runs the real herdr-anywhere.exe as the background Companion, started the way `start`
// starts serve (detached, no console), and has it start what a phone makes it start: hc connect for a terminal, and
// itself for the progress page's commands. Every console program it starts that way would open a console window on
// the desktop for as long as it runs (GitHub #6). The test watches the desktop for new console windows the whole
// time. A console program the test opens with a console of its own first shows the watch sees one. Opt in with
// HM_CONSOLE_LIVE=1 (needs hc and go; CI's Windows jobs).

import (
	"context"
	"fmt"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"github.com/coder/websocket"
	"golang.org/x/sys/windows"

	"herdr-anywhere/companion/internal/store"
)

// consoleWatch records each console window that appears on the desktop after it starts.
type consoleWatch struct {
	mu   sync.Mutex
	seen []string
	stop chan struct{}
	done chan struct{}
}

func watchConsoles(t *testing.T) *consoleWatch {
	t.Helper()
	w := &consoleWatch{stop: make(chan struct{}), done: make(chan struct{})}
	known := map[windows.HWND]bool{}
	var found []windows.HWND
	cb := windows.NewCallback(func(h windows.HWND, _ uintptr) uintptr {
		if windows.IsWindowVisible(h) {
			found = append(found, h)
		}
		return 1
	})
	list := func() []windows.HWND {
		found = found[:0]
		windows.EnumWindows(cb, nil)
		return found
	}
	for _, h := range list() {
		known[h] = true
	}
	go func() {
		defer close(w.done)
		for {
			select {
			case <-w.stop:
				return
			default:
			}
			for _, h := range list() {
				if known[h] {
					continue
				}
				known[h] = true
				class := make([]uint16, 256)
				n, _ := windows.GetClassName(h, &class[0], int32(len(class)))
				name := windows.UTF16ToString(class[:n])
				// conhost's window, and Windows Terminal's where it is the default terminal.
				if name != "ConsoleWindowClass" && name != "CASCADIA_HOSTING_WINDOW_CLASS" {
					continue
				}
				var pid uint32
				windows.GetWindowThreadProcessId(h, &pid)
				w.mu.Lock()
				w.seen = append(w.seen, fmt.Sprintf("%s, process %d (%s)", name, pid, processImage(pid)))
				w.mu.Unlock()
			}
			time.Sleep(2 * time.Millisecond)
		}
	}()
	t.Cleanup(w.end)
	return w
}

func (w *consoleWatch) end() {
	select {
	case <-w.stop:
	default:
		close(w.stop)
	}
	<-w.done
}

// take returns the windows seen since the last take.
func (w *consoleWatch) take() []string {
	w.mu.Lock()
	defer w.mu.Unlock()
	s := w.seen
	w.seen = nil
	return s
}

func processImage(pid uint32) string {
	h, err := windows.OpenProcess(windows.PROCESS_QUERY_LIMITED_INFORMATION, false, pid)
	if err != nil {
		return "gone"
	}
	defer windows.CloseHandle(h)
	buf := make([]uint16, windows.MAX_PATH)
	n := uint32(len(buf))
	if windows.QueryFullProcessImageName(h, 0, &buf[0], &n) != nil {
		return "?"
	}
	return filepath.Base(windows.UTF16ToString(buf[:n]))
}

func TestLiveNoConsoleWindow(t *testing.T) {
	if os.Getenv("HM_CONSOLE_LIVE") != "1" {
		t.Skip("set HM_CONSOLE_LIVE=1 to run the background Companion on this desktop and watch for console windows")
	}
	hc := isolatedHC(t)
	exe := filepath.Join(t.TempDir(), "herdr-anywhere.exe")
	if out, err := exec.Command("go", "build", "-o", exe, ".").CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	dir := shortStateDir(t)
	st, err := store.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	code, err := st.CreateCode("console test", time.Minute)
	if err != nil {
		t.Fatal(err)
	}
	_, token, err := st.Redeem(code, "console test")
	if err != nil {
		t.Fatal(err)
	}
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	addr := ln.Addr().String()
	ln.Close()

	w := watchConsoles(t)
	// The control: a console program given a console of its own, for about as long as the ones serve starts.
	ctl := exec.Command(exe, "--version")
	ctl.SysProcAttr = &syscall.SysProcAttr{CreationFlags: windows.CREATE_NEW_CONSOLE}
	if err := ctl.Run(); err != nil {
		t.Fatalf("the control: %v", err)
	}
	time.Sleep(500 * time.Millisecond)
	if seen := w.take(); len(seen) == 0 {
		t.Fatal("the control opened a console window and the watch saw none: it can't see a flashing window here either")
	} else {
		t.Logf("the control: %s", strings.Join(seen, "; "))
	}

	logPath := filepath.Join(t.TempDir(), "serve.log")
	logFile, err := os.Create(logPath)
	if err != nil {
		t.Fatal(err)
	}
	defer logFile.Close()
	logs := func() string { b, _ := os.ReadFile(logPath); return string(b) }
	serve := exec.Command(exe, "serve", "--listen", addr, "--hc", hc, "--state", dir)
	serve.Stdout, serve.Stderr = logFile, logFile
	serve.SysProcAttr = detached() // as `herdr-anywhere start` starts it
	if err := serve.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { serve.Process.Kill(); serve.Wait() })

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	// A terminal: serve starts hc connect, which starts the core.
	ws, err := dialHC(ctx, addr, token)
	if err != nil {
		t.Fatalf("/hc: %v\nserve:\n%s", err, logs())
	}
	defer ws.CloseNow()
	if err := ws.Write(ctx, websocket.MessageBinary, []byte(`{"jsonrpc":"2.0","id":0,"method":"hello","params":{"framed":true}}`+"\n")); err != nil {
		t.Fatal(err)
	}
	if _, got, err := ws.Read(ctx); err != nil || !strings.Contains(string(got), `"result"`) {
		t.Fatalf("hello: %v %q\nserve:\n%s", err, got, logs())
	}
	// The progress page: serve runs itself, `progress agents`.
	req, _ := http.NewRequestWithContext(ctx, "POST", "http://"+addr+"/api/cli", strings.NewReader(`{"args":["progress","agents"]}`))
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+token)
	r, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("/api/cli: %v\nserve:\n%s", err, logs())
	}
	r.Body.Close()
	if r.StatusCode != http.StatusOK {
		t.Fatalf("/api/cli: %d\nserve:\n%s", r.StatusCode, logs())
	}
	time.Sleep(time.Second)
	w.end()
	if seen := w.take(); len(seen) > 0 {
		t.Fatalf("the background Companion opened console windows: %s\nserve:\n%s", strings.Join(seen, "; "), logs())
	}
}

package proc

import (
	"fmt"
	"os"
	"os/exec"
	"strings"
	"syscall"
	"testing"

	"golang.org/x/sys/windows"
)

// The companion in the background (serve after `start`) has no console. A console program it starts with
// os/exec gets a console of its own, a window that flashes on the screen; one started with Command gets none.
// The test is that companion: this test binary started detached, as `start` starts serve, starting itself again
// as the console program, which says whether it has a console window.
func TestMain(m *testing.M) {
	switch os.Getenv("HM_PROC_ROLE") {
	case "child":
		w, _, _ := windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow").Call()
		if w != 0 {
			fmt.Print("window")
		} else {
			fmt.Print("none")
		}
		os.Exit(0)
	case "detached":
		self, _ := os.Executable()
		var c *exec.Cmd
		if os.Getenv("HM_PROC_HOW") == "proc" {
			c = Command(self)
		} else {
			c = exec.Command(self)
		}
		c.Env = append(os.Environ(), "HM_PROC_ROLE=child")
		out, err := c.Output()
		if err != nil {
			fmt.Print("error: ", err)
		}
		fmt.Print(string(out))
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestNoConsoleWindowFromTheBackground(t *testing.T) {
	self, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for how, want := range map[string]string{"exec": "window", "proc": "none"} {
		c := exec.Command(self)
		c.Env = append(os.Environ(), "HM_PROC_ROLE=detached", "HM_PROC_HOW="+how)
		// As `herdr-anywhere start` starts serve (cmd/herdr-anywhere/proc_windows.go, detached).
		c.SysProcAttr = &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | 0x00000008, HideWindow: true}
		out, err := c.Output()
		if err != nil {
			t.Fatalf("%s: %v", how, err)
		}
		// exec: the control. Were it "none", this test could not see a window, and proc's "none" would prove nothing.
		if got := strings.TrimSpace(string(out)); got != want {
			t.Errorf("a console program started from the background with %s: %q, want %q", how, got, want)
		}
	}
}

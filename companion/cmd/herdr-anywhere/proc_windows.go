package main

import (
	"os"
	"os/exec"
	"syscall"

	"golang.org/x/sys/windows"
)

// detached starts serve in a process group of its own, with no console: it
// outlives the dialog and Herdr, and closing their console doesn't stop it.
func detached() *syscall.SysProcAttr {
	const detachedProcess = 0x00000008 // DETACHED_PROCESS
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess, HideWindow: true}
}

var getConsoleWindow = windows.NewLazySystemDLL("kernel32.dll").NewProc("GetConsoleWindow")

// execSelf: Windows can't run a new program in a running process, so this starts exe with the same arguments,
// environment and output, and the caller ends. A serve with a console (a terminal) leaves the new one in it; one
// without (started by the Herdr dialog) starts it without one too, so no window opens.
func execSelf(exe string) error {
	cmd := exec.Command(exe, os.Args[1:]...)
	cmd.Stdin, cmd.Stdout, cmd.Stderr = os.Stdin, os.Stdout, os.Stderr
	if w, _, _ := getConsoleWindow.Call(); w == 0 {
		cmd.SysProcAttr = detached()
	}
	return cmd.Start()
}

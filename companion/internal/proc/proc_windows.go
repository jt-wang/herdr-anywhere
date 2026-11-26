package proc

import (
	"os/exec"
	"syscall"
)

// createNoWindow: a console program runs without a console window (CREATE_NO_WINDOW); others ignore it.
const createNoWindow = 0x08000000

func hide(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}
}

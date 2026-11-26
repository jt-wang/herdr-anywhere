// Package proc starts the programs the companion runs for itself, such as hc, git and the agents' --version.
//
// On Windows a companion running in the background has no console: `start` detaches serve, and a serve started
// at logon leaves the console Windows opened for it. A console program it starts then gets a console window of
// its own, which flashes on the screen each time. Command and CommandContext start it without one
// (CREATE_NO_WINDOW). A program whose input and output are the user's terminal, such as a command typed in it,
// is started with os/exec as usual.
package proc

import (
	"context"
	"os/exec"
)

// Command is exec.Command, started without a console window on Windows.
func Command(name string, arg ...string) *exec.Cmd {
	cmd := exec.Command(name, arg...)
	hide(cmd)
	return cmd
}

// CommandContext is exec.CommandContext, started without a console window on Windows.
func CommandContext(ctx context.Context, name string, arg ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, name, arg...)
	hide(cmd)
	return cmd
}

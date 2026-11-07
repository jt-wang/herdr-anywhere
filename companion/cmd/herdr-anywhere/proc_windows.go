package main

import (
	"syscall"

	"herdr-anywhere/companion/internal/update"
)

// detached starts serve in a process group of its own, with no console: it
// outlives the dialog and Herdr, and closing their console doesn't stop it.
func detached() *syscall.SysProcAttr {
	const detachedProcess = 0x00000008 // DETACHED_PROCESS
	return &syscall.SysProcAttr{CreationFlags: syscall.CREATE_NEW_PROCESS_GROUP | detachedProcess, HideWindow: true}
}

// execSelf: Windows can't replace a running process, and releases carry no
// Windows file (update.Supported is false here), so serve never installs an
// update to restart into.
func execSelf(string) error { return update.ErrNotHere }

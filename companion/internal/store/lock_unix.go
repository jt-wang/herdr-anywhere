//go:build !windows

package store

import (
	"os"
	"syscall"
)

// lockFile waits for an exclusive lock on f; another process's update waits for it.
func lockFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_EX) }

func unlockFile(f *os.File) error { return syscall.Flock(int(f.Fd()), syscall.LOCK_UN) }

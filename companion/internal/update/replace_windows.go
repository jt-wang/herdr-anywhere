package update

import (
	"errors"
	"fmt"
	"os"
	"syscall"
	"time"
)

// Windows on Arm keeps an x64 program's file open a moment after it ran (Apply runs the new file with --version
// first), so renaming it can fail with a sharing violation, or access denied, that passes within a second or two.
const (
	errAccessDenied     syscall.Errno = 5
	errSharingViolation syscall.Errno = 32
)

func held(err error) bool {
	return errors.Is(err, errSharingViolation) || errors.Is(err, errAccessDenied)
}

// replace puts file in exe's place. Windows doesn't let a running program's file be written over or removed, but it
// can be renamed: the old one goes aside as exe + ".old" (or exe + ".old-<n>" while an older one is still in use;
// RemoveOld clears what a running program kept at a later start), and comes back if the new one can't take its place.
func replace(file, exe string) error {
	old := exe + ".old"
	if err := os.Remove(old); err != nil && !os.IsNotExist(err) {
		old = fmt.Sprintf("%s.old-%d", exe, time.Now().UnixNano())
	}
	if err := whileBusy(func() error { return os.Rename(exe, old) }, held, 10*time.Second); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := whileBusy(func() error { return os.Rename(file, exe) }, held, 10*time.Second); err != nil {
		os.Rename(old, exe)
		return err
	}
	os.Remove(old) // goes now unless the old program is still running
	return nil
}

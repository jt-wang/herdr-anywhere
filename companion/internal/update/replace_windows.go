package update

import (
	"fmt"
	"os"
	"time"
)

// replace puts file in exe's place. Windows doesn't let a running program's file be written over or removed, but it
// can be renamed: the old one goes aside as exe + ".old" (or exe + ".old-<n>" while an older one is still in use;
// RemoveOld clears what a running program kept at a later start), and comes back if the new one can't take its place.
func replace(file, exe string) error {
	old := exe + ".old"
	if err := os.Remove(old); err != nil && !os.IsNotExist(err) {
		old = fmt.Sprintf("%s.old-%d", exe, time.Now().UnixNano())
	}
	if err := os.Rename(exe, old); err != nil && !os.IsNotExist(err) {
		return err
	}
	if err := os.Rename(file, exe); err != nil {
		os.Rename(old, exe)
		return err
	}
	os.Remove(old) // goes now unless the old program is still running
	return nil
}

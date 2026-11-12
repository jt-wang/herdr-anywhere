//go:build !windows

package chat

import (
	"os"
	"syscall"
)

// Inode tells a session file replaced by another file of the same size and time from the one read before.
func Inode(st os.FileInfo) uint64 {
	if s, ok := st.Sys().(*syscall.Stat_t); ok {
		return uint64(s.Ino)
	}
	return 0
}

package chat

import "os"

// Inode: Windows' FileInfo carries no file number, so a session file is told apart by its size and time alone.
func Inode(os.FileInfo) uint64 { return 0 }

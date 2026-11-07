//go:build !windows

package update

import "os"

// replace puts file in exe's place; a running exe keeps its old file until it ends.
func replace(file, exe string) error { return os.Rename(file, exe) }

//go:build !windows

package tailcat

import (
	"errors"
	"os"
	"syscall"
)

// restrictSocket lets only this user connect.
func restrictSocket(p string) error { return os.Chmod(p, 0o600) }

// noServe says a failed request found no serve: no socket, or one a crashed serve left behind.
func noServe(err error) bool {
	return errors.Is(err, syscall.ENOENT) || errors.Is(err, syscall.ECONNREFUSED)
}

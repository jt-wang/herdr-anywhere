package tailcat

import (
	"errors"
	"net"
)

// restrictSocket: on Windows os.Chmod only sets the read-only attribute, so it
// adds nothing. Who may connect is the socket file's ACL, inherited from the
// state dir in the person's profile; not yet checked on a Windows machine.
func restrictSocket(string) error { return nil }

// noServe: Windows' errors for a missing or abandoned unix socket are
// Winsock codes that syscall.ENOENT and ECONNREFUSED don't match, so any
// failure to connect means no serve; the request never reached one.
func noServe(err error) bool {
	var op *net.OpError
	return errors.As(err, &op) && op.Op == "dial"
}

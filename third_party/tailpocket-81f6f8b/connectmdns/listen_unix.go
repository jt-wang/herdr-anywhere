//go:build !windows && !js && !wasip1

package connectmdns

import (
	"context"
	"net"
	"syscall"

	"golang.org/x/sys/unix"
)

// listenShared binds 0.0.0.0:5353 alongside the system responder.
func listenShared() (net.PacketConn, error) {
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var serr error
		err := c.Control(func(fd uintptr) {
			if serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEADDR, 1); serr != nil {
				return
			}
			serr = unix.SetsockoptInt(int(fd), unix.SOL_SOCKET, unix.SO_REUSEPORT, 1)
		})
		if err != nil {
			return err
		}
		return serr
	}}
	return lc.ListenPacket(context.Background(), "udp4", "0.0.0.0:5353")
}

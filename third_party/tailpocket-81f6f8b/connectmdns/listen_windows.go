//go:build windows

package connectmdns

import (
	"context"
	"net"
	"syscall"
)

// listenShared binds 0.0.0.0:5353 with SO_REUSEADDR, Windows' way of sharing the port.
func listenShared() (net.PacketConn, error) {
	lc := net.ListenConfig{Control: func(_, _ string, c syscall.RawConn) error {
		var serr error
		err := c.Control(func(fd uintptr) {
			serr = syscall.SetsockoptInt(syscall.Handle(fd), syscall.SOL_SOCKET, syscall.SO_REUSEADDR, 1)
		})
		if err != nil {
			return err
		}
		return serr
	}}
	return lc.ListenPacket(context.Background(), "udp4", "0.0.0.0:5353")
}

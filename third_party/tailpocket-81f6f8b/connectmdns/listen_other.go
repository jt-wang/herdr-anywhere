//go:build js || wasip1

package connectmdns

import (
	"errors"
	"net"
)

// Browsers and WASI have no multicast sockets.
func listenShared() (net.PacketConn, error) {
	return nil, errors.New("connectmdns: not available on this platform")
}

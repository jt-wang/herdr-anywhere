package connectrelay

import (
	"net"
	"net/netip"
	"sync"
)

// Listener wraps ln so the relay holds at most MaxConns TCP connections at
// once, and at most MaxConnsPerAddr from one source address (AddrKey;
// TrustProxy addresses stand for many users and have no per-address
// limit). It counts every connection from accept to close: ones that have
// not sent a request yet and ones upgraded to WebSocket alike. A
// connection over a limit is closed at once, before anything is read.
func (s *Server) Listener(ln net.Listener) net.Listener {
	return &limitListener{Listener: ln, s: s}
}

type limitListener struct {
	net.Listener
	s *Server
}

func (l *limitListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		if lc := l.s.admit(c); lc != nil {
			return lc, nil
		}
	}
}

// admit counts c in, or closes it and returns nil.
func (s *Server) admit(c net.Conn) net.Conn {
	var key netip.Addr
	perAddr := false
	if ap, err := netip.ParseAddrPort(c.RemoteAddr().String()); err == nil {
		a := ap.Addr().Unmap()
		key = AddrKey(a)
		perAddr = !trusted(a, s.cfg.TrustProxy)
	}
	s.connMu.Lock()
	why := ""
	switch {
	case s.conns >= s.cfg.MaxConns:
		why = "max-conns"
	case perAddr && s.perAddr[key] >= s.cfg.MaxConnsPerAddr:
		why = "addr-conns"
	}
	if why != "" {
		s.connMu.Unlock()
		s.refusedWhy("", 0, why) // before Close: a test sees the close, then reads the log
		c.Close()
		return nil
	}
	s.conns++
	if perAddr {
		s.perAddr[key]++
	}
	s.connMu.Unlock()
	return &countedConn{Conn: c, release: func() {
		s.connMu.Lock()
		s.conns--
		if perAddr {
			if s.perAddr[key]--; s.perAddr[key] <= 0 {
				delete(s.perAddr, key)
			}
		}
		s.connMu.Unlock()
	}}
}

func trusted(a netip.Addr, trust []netip.Prefix) bool {
	for _, p := range trust {
		if p.Contains(a) {
			return true
		}
	}
	return false
}

// countedConn gives its slot back on the first Close.
type countedConn struct {
	net.Conn
	once    sync.Once
	release func()
}

func (c *countedConn) Close() error {
	err := c.Conn.Close()
	c.once.Do(c.release)
	return err
}

// CloseWrite keeps net/http's half-close on a plain TCP connection.
func (c *countedConn) CloseWrite() error {
	if cw, ok := c.Conn.(interface{ CloseWrite() error }); ok {
		return cw.CloseWrite()
	}
	return nil
}

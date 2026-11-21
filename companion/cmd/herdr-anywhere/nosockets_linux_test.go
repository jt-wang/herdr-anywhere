package main

import (
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// noTailcatSockets returns a check that every socket this process opened since, and still holds, stays on this
// computer: TCP between loopback or this computer's own addresses, and no UDP at all (Tailcat's WireGuard and
// STUN are UDP). It reads the process's socket inodes from /proc/self/fd and their addresses from /proc/net.
func noTailcatSockets(t *testing.T) func() {
	t.Helper()
	before := ownSocketInodes(t)
	return func() {
		t.Helper()
		own := map[netip.Addr]bool{}
		if addrs, err := net.InterfaceAddrs(); err == nil {
			for _, a := range addrs {
				if p, err := netip.ParsePrefix(a.String()); err == nil {
					own[p.Addr().Unmap()] = true
				}
			}
		}
		local := func(a netip.Addr) bool { return a.IsLoopback() || a.IsUnspecified() || own[a] }
		now := ownSocketInodes(t)
		n := 0
		for _, proto := range []string{"tcp", "tcp6", "udp", "udp6"} {
			b, err := os.ReadFile("/proc/net/" + proto)
			if err != nil {
				continue
			}
			for _, line := range strings.Split(string(b), "\n")[1:] {
				f := strings.Fields(line)
				if len(f) < 10 || !now[f[9]] || before[f[9]] {
					continue
				}
				n++
				la, ra := procAddr(f[1]), procAddr(f[2])
				switch {
				case strings.HasPrefix(proto, "udp"):
					t.Errorf("a UDP socket without Tailcat: %s %s -> %s", proto, la, ra)
				case !local(la.Addr()) || !local(ra.Addr()):
					t.Errorf("a socket off this computer without Tailcat: %s %s -> %s", proto, la, ra)
				}
			}
		}
		t.Logf("%d sockets opened since serve started, all on this computer", n)
	}
}

// ownSocketInodes are the inodes of the sockets this process holds open.
func ownSocketInodes(t *testing.T) map[string]bool {
	t.Helper()
	fds, err := os.ReadDir("/proc/self/fd")
	if err != nil {
		t.Fatal(err)
	}
	out := map[string]bool{}
	for _, fd := range fds {
		l, err := os.Readlink(filepath.Join("/proc/self/fd", fd.Name()))
		if err == nil && strings.HasPrefix(l, "socket:[") {
			out[strings.TrimSuffix(strings.TrimPrefix(l, "socket:["), "]")] = true
		}
	}
	return out
}

// procAddr reads /proc/net's "0100007F:1F90": the address in host (little-endian) 32-bit words, the port in hex.
func procAddr(s string) netip.AddrPort {
	h, p, ok := strings.Cut(s, ":")
	b, err := hex.DecodeString(h)
	if !ok || err != nil || (len(b) != 4 && len(b) != 16) {
		return netip.AddrPort{}
	}
	for i := 0; i < len(b); i += 4 {
		b[i], b[i+1], b[i+2], b[i+3] = b[i+3], b[i+2], b[i+1], b[i]
	}
	a, _ := netip.AddrFromSlice(b)
	var port uint16
	fmt.Sscanf(p, "%X", &port)
	return netip.AddrPortFrom(a.Unmap(), port)
}

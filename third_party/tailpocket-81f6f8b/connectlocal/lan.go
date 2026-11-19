package connectlocal

import (
	"net"

	"github.com/jt-wang/tailpocket/internal/lan"
)

// Binding is one Wi-Fi/LAN address the host serves local pairing on.
type Binding struct {
	IP   net.IP
	Mask net.IPMask
}

// Contains reports whether ip is an IPv4 address on b's subnet.
func (b Binding) Contains(ip net.IP) bool {
	v4, own := ip.To4(), b.IP.To4()
	if v4 == nil || own == nil || len(b.Mask) == 0 {
		return false
	}
	mask := b.Mask
	if len(mask) == net.IPv6len {
		mask = mask[12:]
	}
	return own.Mask(mask).Equal(v4.Mask(mask))
}

// LANAddresses returns the private IPv4 addresses (RFC 1918 and link-local) of this machine's
// physical interfaces that are up.
func LANAddresses() []Binding {
	var in []iface
	for _, i := range lan.Interfaces() {
		var addrs []net.Addr
		for _, n := range i.Networks {
			addrs = append(addrs, n)
		}
		in = append(in, iface{name: i.Interface.Name, addrs: addrs})
	}
	return lanAddresses(in)
}

type iface struct {
	name  string
	addrs []net.Addr
}

func lanAddresses(in []iface) []Binding {
	var out []Binding
	for _, i := range in {
		if !lan.Physical(i.name) {
			continue
		}
		for _, n := range lan.Networks(i.addrs) {
			out = append(out, Binding{IP: n.IP, Mask: n.Mask})
		}
	}
	return out
}

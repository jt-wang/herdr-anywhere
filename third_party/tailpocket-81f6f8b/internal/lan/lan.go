// Package lan finds the Wi-Fi/Ethernet interfaces a phone on the same network can reach.
package lan

import (
	"net"
	"regexp"
)

// Tunnels, VPNs, AirDrop/peer-to-peer links and VM bridges never carry the phone's Wi-Fi.
var virtual = regexp.MustCompile(`(?i)^(lo|utun|ipsec|ppp|gif|stf|awdl|llw|anpi|bridge|vmnet|vboxnet|tun|tap|wg|tailscale|zt|docker|veth|virbr)`)

// Physical reports whether an interface name can be a Wi-Fi/Ethernet link.
func Physical(name string) bool { return !virtual.MatchString(name) }

// PrivateIPv4 reports RFC 1918 and link-local IPv4 addresses.
func PrivateIPv4(ip net.IP) bool {
	v4 := ip.To4()
	if v4 == nil {
		return false
	}
	return v4[0] == 10 || (v4[0] == 172 && v4[1]&0xf0 == 16) || (v4[0] == 192 && v4[1] == 168) || (v4[0] == 169 && v4[1] == 254)
}

// Iface is an up, multicast-capable physical interface with its private IPv4 networks.
type Iface struct {
	Interface net.Interface
	Networks  []*net.IPNet // IPv4, 4-byte IP and mask
}

// Interfaces lists the interfaces local pairing and Bonjour use.
func Interfaces() []Iface {
	list, err := net.Interfaces()
	if err != nil {
		return nil
	}
	var out []Iface
	for _, i := range list {
		if i.Flags&net.FlagUp == 0 || i.Flags&net.FlagLoopback != 0 || i.Flags&net.FlagMulticast == 0 || !Physical(i.Name) {
			continue
		}
		addrs, err := i.Addrs()
		if err != nil {
			continue
		}
		if nets := Networks(addrs); len(nets) > 0 {
			out = append(out, Iface{Interface: i, Networks: nets})
		}
	}
	return out
}

// Networks keeps the private IPv4 networks of addrs, normalized to 4-byte form.
func Networks(addrs []net.Addr) []*net.IPNet {
	var out []*net.IPNet
	for _, a := range addrs {
		n, ok := a.(*net.IPNet)
		if !ok || !PrivateIPv4(n.IP) {
			continue
		}
		mask := n.Mask
		if len(mask) == net.IPv6len {
			mask = mask[12:]
		}
		out = append(out, &net.IPNet{IP: n.IP.To4(), Mask: mask})
	}
	return out
}

package connectmobile

import (
	"net"

	"tailscale.com/net/netmon"
)

// Android apps targeting SDK 30+ may not bind netlink, so Go's net.Interfaces fails there and
// Tailcat's network monitor refuses to start. getifaddrs(3) is still allowed (it is what
// java.net.NetworkInterface uses); interfaces_android.go feeds its rows through interfacesFromRows.
// Adapted from DSH Mobile (MIT, see NOTICE).

type ifaddrRow struct {
	name  string
	index int
	flags uint32 // Linux IFF_* bits
	ip    net.IP
	mask  net.IPMask
}

func linuxFlags(bits uint32) net.Flags {
	var f net.Flags
	for bit, flag := range map[uint32]net.Flags{0x1: net.FlagUp, 0x2: net.FlagBroadcast, 0x8: net.FlagLoopback,
		0x10: net.FlagPointToPoint, 0x40: net.FlagRunning, 0x1000: net.FlagMulticast} {
		if bits&bit != 0 {
			f |= flag
		}
	}
	return f
}

// interfacesFromRows groups rows by name in first-seen order. AltAddrs is never nil, so netmon
// does not fall back to the denied netlink path for an interface without addresses.
func interfacesFromRows(rows []ifaddrRow) []netmon.Interface {
	var out []netmon.Interface
	index := map[string]int{}
	for _, r := range rows {
		i, ok := index[r.name]
		if !ok {
			i = len(out)
			index[r.name] = i
			out = append(out, netmon.Interface{Interface: &net.Interface{Index: r.index, Name: r.name, Flags: linuxFlags(r.flags)}, AltAddrs: []net.Addr{}})
		}
		if r.ip == nil {
			continue
		}
		ip := r.ip
		if v4 := ip.To4(); v4 != nil {
			ip = v4
		}
		out[i].AltAddrs = append(out[i].AltAddrs, &net.IPNet{IP: ip, Mask: r.mask})
	}
	return out
}

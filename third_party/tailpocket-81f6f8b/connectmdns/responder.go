package connectmdns

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"net"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/ipv4"

	"github.com/jt-wang/tailpocket/internal/lan"
)

func lanInterfaces() []lan.Iface { return lan.Interfaces() }

// AdvertiseBuiltin answers queries for s itself, on every Wi-Fi/Ethernet interface, sharing port
// 5353 with any system responder (SO_REUSEPORT). Conflict probing is not done: pick an instance
// name unlikely to collide, such as the computer's name.
func AdvertiseBuiltin(s Service) (func(), error) {
	if err := s.validate(); err != nil {
		return nil, err
	}
	ifaces := lanInterfaces()
	if len(ifaces) == 0 {
		return nil, errors.New("connectmdns: no Wi-Fi/LAN interface")
	}
	conn, err := listenShared()
	if err != nil {
		return nil, err
	}
	pc := ipv4.NewPacketConn(conn)
	byIndex := map[int][]net.IP{}
	var joined []lan.Iface
	for _, i := range ifaces {
		ifi := i.Interface
		if pc.JoinGroup(&ifi, &net.UDPAddr{IP: group}) != nil {
			continue
		}
		for _, n := range i.Networks {
			byIndex[ifi.Index] = append(byIndex[ifi.Index], n.IP)
		}
		joined = append(joined, i)
	}
	if len(joined) == 0 {
		conn.Close()
		return nil, errors.New("connectmdns: could not join the multicast group")
	}
	_ = pc.SetControlMessage(ipv4.FlagInterface, true)
	_ = pc.SetMulticastLoopback(true)
	_ = pc.SetMulticastTTL(255)
	r := newRecords(s, hostLabel(s.Instance)+".local.")
	a := &advertiser{pc: pc, conn: conn, r: r, ifaces: joined, byIndex: byIndex, done: make(chan struct{})}
	if !a.broadcast(false) {
		conn.Close()
		return nil, ErrMulticastBlocked
	}
	a.wg.Add(2)
	go a.serve()
	go a.announce()
	return a.stop, nil
}

type advertiser struct {
	pc      *ipv4.PacketConn
	conn    net.PacketConn
	r       *records
	ifaces  []lan.Iface
	byIndex map[int][]net.IP
	writeMu sync.Mutex
	done    chan struct{}
	once    sync.Once
	wg      sync.WaitGroup
}

func (a *advertiser) serve() {
	defer a.wg.Done()
	buf := make([]byte, maxPacket)
	for {
		n, cm, src, err := a.pc.ReadFrom(buf)
		if err != nil {
			return
		}
		from, _ := src.(*net.UDPAddr)
		if from == nil {
			continue
		}
		var ips []net.IP
		index := 0
		if cm != nil {
			index = cm.IfIndex
			ips = a.byIndex[index]
		}
		if ips == nil {
			ips = a.allIPs()
		}
		reply, unicast, ok := a.r.respond(buf[:n], from.Port, ips)
		if !ok {
			continue
		}
		if unicast {
			_ = a.write(reply, from, 0)
		} else {
			_ = a.write(reply, &net.UDPAddr{IP: group, Port: mdnsPort}, index)
		}
	}
}

func (a *advertiser) allIPs() []net.IP {
	var out []net.IP
	for _, ips := range a.byIndex {
		out = append(out, ips...)
	}
	return out
}

func (a *advertiser) write(b []byte, to *net.UDPAddr, index int) error {
	a.writeMu.Lock()
	defer a.writeMu.Unlock()
	if index != 0 {
		if ifi, err := net.InterfaceByIndex(index); err == nil {
			_ = a.pc.SetMulticastInterface(ifi)
		}
	}
	_, err := a.pc.WriteTo(b, nil, to)
	return err
}

// announce sends unsolicited responses (RFC 6762 §8.3) so caches learn the service quickly.
func (a *advertiser) announce() {
	defer a.wg.Done()
	for _, wait := range []time.Duration{time.Second, 2 * time.Second} {
		select {
		case <-a.done:
			return
		case <-time.After(wait):
		}
		a.broadcast(false)
	}
}

// broadcast reports whether the announcement left on at least one interface.
func (a *advertiser) broadcast(goodbye bool) bool {
	sent := false
	for _, i := range a.ifaces {
		var ips []net.IP
		for _, n := range i.Networks {
			ips = append(ips, n.IP)
		}
		answers := []dnsmessage.Resource{a.r.ptr(serviceTTL), a.r.srv(hostTTL, true), a.r.txt(serviceTTL, true)}
		answers = append(answers, a.r.addresses(ips, hostTTL, true)...)
		if goodbye {
			for k := range answers {
				answers[k].Header.TTL = 0
			}
		}
		out, err := build(dnsmessage.Header{Response: true, Authoritative: true}, nil, answers, nil)
		if err != nil {
			continue
		}
		if a.write(out, &net.UDPAddr{IP: group, Port: mdnsPort}, i.Interface.Index) == nil {
			sent = true
		}
	}
	return sent
}

func (a *advertiser) stop() {
	a.once.Do(func() {
		close(a.done)
		a.broadcast(true)
		_ = a.conn.Close()
		a.wg.Wait()
	})
}

// hostLabel is a DNS-safe, collision-resistant host label derived from the instance name.
func hostLabel(instance string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(instance) {
		switch {
		case r >= 'a' && r <= 'z', r >= '0' && r <= '9':
			b.WriteRune(r)
		case b.Len() > 0 && !strings.HasSuffix(b.String(), "-"):
			b.WriteByte('-')
		}
		if b.Len() >= 40 {
			break
		}
	}
	label := strings.Trim(b.String(), "-")
	if label == "" {
		label = "host"
	}
	suffix := make([]byte, 3)
	_, _ = rand.Read(suffix)
	return label + "-" + hex.EncodeToString(suffix)
}

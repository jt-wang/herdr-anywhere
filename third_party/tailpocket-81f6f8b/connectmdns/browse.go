package connectmdns

import (
	"context"
	"errors"
	"net"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/net/dns/dnsmessage"
	"golang.org/x/net/ipv4"
)

// Browse finds instances of typ (e.g. "_myapp._tcp") until ctx ends, then closes the channel.
// Each resolved instance is sent once, and again if its address or port changes. It uses the
// system daemon where there is one (it works even where this process may not send multicast,
// as under macOS Local Network privacy) and the built-in querier otherwise.
func Browse(ctx context.Context, typ string) (<-chan Entry, error) {
	if SystemBrowseAvailable() {
		return BrowseSystem(ctx, typ)
	}
	return BrowseBuiltin(ctx, typ)
}

// BrowseBuiltin browses with legacy unicast mDNS queries sent from this process.
func BrowseBuiltin(ctx context.Context, typ string) (<-chan Entry, error) {
	if !ValidType(typ) {
		return nil, errors.New("connectmdns: service type must look like _name._tcp")
	}
	conn, err := net.ListenPacket("udp4", "0.0.0.0:0")
	if err != nil {
		return nil, err
	}
	pc := ipv4.NewPacketConn(conn)
	_ = pc.SetMulticastLoopback(true)
	_ = pc.SetMulticastTTL(255)
	b := &browser{pc: pc, typ: dnsmessage.MustNewName(typ + ".local."), typText: typ, out: make(chan Entry, 8),
		found: map[string]*partial{}, hosts: map[string][]net.IP{}, sent: map[string]string{}}
	probe, err := build(dnsmessage.Header{}, []dnsmessage.Question{{Name: b.typ, Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET}}, nil, nil)
	if err != nil {
		conn.Close()
		return nil, err
	}
	if !b.send(probe) {
		conn.Close()
		return nil, ErrMulticastBlocked
	}
	go func() { <-ctx.Done(); _ = conn.Close() }()
	go b.query(ctx)
	go b.read(ctx)
	return b.out, nil
}

type partial struct {
	name    dnsmessage.Name
	host    string
	port    int
	txt     map[string]string
	sources []net.IP
}

type browser struct {
	pc      *ipv4.PacketConn
	typ     dnsmessage.Name
	typText string
	out     chan Entry
	mu      sync.Mutex
	found   map[string]*partial // lowercased instance FQDN
	hosts   map[string][]net.IP // lowercased host FQDN
	sent    map[string]string
}

func (b *browser) query(ctx context.Context) {
	wait := 0 * time.Second
	for {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		qs := []dnsmessage.Question{{Name: b.typ, Type: dnsmessage.TypePTR, Class: dnsmessage.ClassINET}}
		b.mu.Lock()
		for _, p := range b.found {
			if p.port == 0 || p.txt == nil {
				qs = append(qs, dnsmessage.Question{Name: p.name, Type: dnsmessage.TypeALL, Class: dnsmessage.ClassINET})
			}
			if p.host != "" && len(b.hosts[p.host]) == 0 {
				if n, err := dnsmessage.NewName(p.host); err == nil {
					qs = append(qs, dnsmessage.Question{Name: n, Type: dnsmessage.TypeA, Class: dnsmessage.ClassINET})
				}
			}
		}
		b.mu.Unlock()
		if packet, err := build(dnsmessage.Header{}, qs, nil, nil); err == nil {
			b.send(packet)
		}
		if wait < 3*time.Second {
			wait += time.Second
		}
	}
}

// send reports whether the query left on at least one interface.
func (b *browser) send(packet []byte) bool {
	to := &net.UDPAddr{IP: group, Port: mdnsPort}
	ifaces := lanInterfaces()
	if len(ifaces) == 0 {
		_, err := b.pc.WriteTo(packet, nil, to)
		return err == nil
	}
	sent := false
	for _, i := range ifaces {
		ifi := i.Interface
		_ = b.pc.SetMulticastInterface(&ifi)
		if _, err := b.pc.WriteTo(packet, nil, to); err == nil {
			sent = true
		}
	}
	return sent
}

func (b *browser) read(ctx context.Context) {
	defer close(b.out)
	buf := make([]byte, maxPacket)
	for {
		n, _, src, err := b.pc.ReadFrom(buf)
		if err != nil {
			return
		}
		from, _ := src.(*net.UDPAddr)
		for _, e := range b.absorb(buf[:n], from) {
			select {
			case b.out <- e:
			case <-ctx.Done():
				return
			}
		}
	}
}

// absorb merges one response into what is known and returns entries that became complete or changed.
func (b *browser) absorb(packet []byte, from *net.UDPAddr) []Entry {
	var p dnsmessage.Parser
	h, err := p.Start(packet)
	if err != nil || !h.Response {
		return nil
	}
	if err := p.SkipAllQuestions(); err != nil {
		return nil
	}
	var all []dnsmessage.Resource
	for _, section := range []func() ([]dnsmessage.Resource, error){p.AllAnswers, p.AllAuthorities, p.AllAdditionals} {
		rs, err := section()
		if err != nil {
			break
		}
		all = append(all, rs...)
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	suffix := "." + strings.ToLower(b.typ.String())
	get := func(name dnsmessage.Name) *partial {
		key := strings.ToLower(name.String())
		if !strings.HasSuffix(key, suffix) || len(key) == len(suffix) {
			return nil
		}
		if b.found[key] == nil {
			b.found[key] = &partial{name: name}
		}
		return b.found[key]
	}
	for _, r := range all {
		switch body := r.Body.(type) {
		case *dnsmessage.PTRResource:
			if same(r.Header.Name, b.typ) && r.Header.TTL > 0 {
				get(body.PTR)
			}
		case *dnsmessage.SRVResource:
			if pt := get(r.Header.Name); pt != nil {
				pt.host, pt.port = strings.ToLower(body.Target.String()), int(body.Port)
				if from != nil {
					pt.sources = append(pt.sources[:0], from.IP.To4())
				}
			}
		case *dnsmessage.TXTResource:
			if pt := get(r.Header.Name); pt != nil {
				pt.txt = map[string]string{}
				for _, s := range body.TXT {
					if s == "" {
						continue
					}
					k, v, _ := strings.Cut(s, "=")
					pt.txt[strings.ToLower(k)] = v
				}
			}
		case *dnsmessage.AResource:
			host := strings.ToLower(r.Header.Name.String())
			ip := net.IP(append([]byte{}, body.A[:]...))
			if !containsIP(b.hosts[host], ip) {
				b.hosts[host] = append(b.hosts[host], ip)
			}
		}
	}
	var out []Entry
	for key, pt := range b.found {
		if pt.port == 0 {
			continue
		}
		ips := b.hosts[pt.host]
		if len(ips) == 0 {
			ips = pt.sources
		}
		if len(ips) == 0 {
			continue
		}
		instance := pt.name.String()
		instance = instance[:len(instance)-len(suffix)]
		e := Entry{Instance: instance, Type: b.typText, Host: pt.host, Port: pt.port, IPv4: sortedIPs(ips), TXT: pt.txt}
		if e.TXT == nil {
			e.TXT = map[string]string{}
		}
		sig := e.Addr() + "|" + e.Host
		if b.sent[key] != sig {
			b.sent[key] = sig
			out = append(out, e)
		}
	}
	return out
}

func containsIP(list []net.IP, ip net.IP) bool {
	for _, x := range list {
		if x.Equal(ip) {
			return true
		}
	}
	return false
}

func sortedIPs(in []net.IP) []net.IP {
	out := append([]net.IP{}, in...)
	sort.Slice(out, func(i, j int) bool { return out[i].String() < out[j].String() })
	return out
}

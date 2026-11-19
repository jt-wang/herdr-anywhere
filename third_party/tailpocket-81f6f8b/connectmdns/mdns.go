// Package connectmdns advertises and browses one DNS-SD (Bonjour) service on the local network.
//
// Advertise uses the operating system's Bonjour daemon when one is available (dns-sd on macOS,
// avahi-publish-service on Linux) and otherwise a small built-in responder. Browse is built in
// and uses legacy unicast queries (RFC 6762 §6.7), so it never binds port 5353.
package connectmdns

import (
	"errors"
	"fmt"
	"net"
	"regexp"
	"strings"
	"unicode"
	"unicode/utf8"

	"golang.org/x/net/dns/dnsmessage"
)

const (
	mdnsPort        = 5353
	servicesName    = "_services._dns-sd._udp.local."
	cacheFlush      = 0x8000
	hostTTL         = 120
	serviceTTL      = 4500
	legacyMaxTTL    = 10
	maxPacket       = 9000
	dnsClassUnicast = 0x8000
)

var group = net.IPv4(224, 0, 0, 251)

// ErrMulticastBlocked means this process may not send multicast. On macOS that is Local Network
// privacy: allow the app in System Settings › Privacy & Security › Local Network, or use the system
// daemon (Advertise and Browse do so by default).
var ErrMulticastBlocked = errors.New("connectmdns: this process may not send multicast (macOS: Local Network permission)")

// Service is one DNS-SD registration, e.g. Instance "Studio Mac", Type "_myapp._tcp".
type Service struct {
	Instance string
	Type     string
	Port     int
	TXT      []string // "key=value" strings
}

// Entry is one resolved service found by Browse.
type Entry struct {
	Instance string
	Type     string
	Host     string
	Port     int
	IPv4     []net.IP
	TXT      map[string]string
}

// Addr returns "ip:port" for the first IPv4 address.
func (e Entry) Addr() string {
	if len(e.IPv4) == 0 {
		return ""
	}
	return net.JoinHostPort(e.IPv4[0].String(), fmt.Sprint(e.Port))
}

var serviceType = regexp.MustCompile(`^_[A-Za-z0-9](?:[A-Za-z0-9-]{0,13}[A-Za-z0-9])?\._tcp$`)

// ValidType reports whether typ is "_name._tcp" with an RFC 6335 service name (1–15 characters).
func ValidType(typ string) bool { return serviceType.MatchString(typ) }

func (s Service) validate() error {
	if !ValidType(s.Type) {
		return errors.New("connectmdns: service type must look like _name._tcp")
	}
	if s.Instance == "" || len(s.Instance) > 63 || !utf8.ValidString(s.Instance) || strings.ContainsAny(s.Instance, ".\\") {
		return errors.New("connectmdns: instance must be 1–63 bytes without dots")
	}
	for _, r := range s.Instance {
		if unicode.IsControl(r) {
			return errors.New("connectmdns: instance has control characters")
		}
	}
	if s.Port < 1 || s.Port > 65535 {
		return errors.New("connectmdns: invalid port")
	}
	for _, t := range s.TXT {
		if len(t) > 255 {
			return errors.New("connectmdns: TXT string over 255 bytes")
		}
	}
	return nil
}

// CleanInstance turns any display name into a valid instance name.
func CleanInstance(name string) string {
	name = strings.Map(func(r rune) rune {
		if unicode.IsControl(r) || r == '.' || r == '\\' {
			return ' '
		}
		return r
	}, name)
	name = strings.Join(strings.Fields(name), " ")
	for len(name) > 63 {
		_, size := utf8.DecodeLastRuneInString(name)
		name = name[:len(name)-size]
	}
	return strings.TrimSpace(name)
}

// records answers questions about one service.
type records struct {
	svc                          Service
	typeName, instName, hostName dnsmessage.Name
}

func newRecords(s Service, host string) *records {
	return &records{
		svc:      s,
		typeName: dnsmessage.MustNewName(s.Type + ".local."),
		instName: dnsmessage.MustNewName(s.Instance + "." + s.Type + ".local."),
		hostName: dnsmessage.MustNewName(host),
	}
}

func same(a, b dnsmessage.Name) bool { return strings.EqualFold(a.String(), b.String()) }

func header(name dnsmessage.Name, typ dnsmessage.Type, ttl uint32, unique bool) dnsmessage.ResourceHeader {
	class := dnsmessage.ClassINET
	if unique {
		class |= cacheFlush
	}
	return dnsmessage.ResourceHeader{Name: name, Type: typ, Class: class, TTL: ttl}
}

func (r *records) srv(ttl uint32, unique bool) dnsmessage.Resource {
	return dnsmessage.Resource{Header: header(r.instName, dnsmessage.TypeSRV, ttl, unique),
		Body: &dnsmessage.SRVResource{Target: r.hostName, Port: uint16(r.svc.Port)}}
}

func (r *records) txt(ttl uint32, unique bool) dnsmessage.Resource {
	txt := r.svc.TXT
	if len(txt) == 0 {
		txt = []string{""}
	}
	return dnsmessage.Resource{Header: header(r.instName, dnsmessage.TypeTXT, ttl, unique), Body: &dnsmessage.TXTResource{TXT: txt}}
}

func (r *records) addresses(ips []net.IP, ttl uint32, unique bool) []dnsmessage.Resource {
	var out []dnsmessage.Resource
	for _, ip := range ips {
		v4 := ip.To4()
		if v4 == nil {
			continue
		}
		var a [4]byte
		copy(a[:], v4)
		out = append(out, dnsmessage.Resource{Header: header(r.hostName, dnsmessage.TypeA, ttl, unique), Body: &dnsmessage.AResource{A: a}})
	}
	return out
}

func (r *records) ptr(ttl uint32) dnsmessage.Resource {
	return dnsmessage.Resource{Header: header(r.typeName, dnsmessage.TypePTR, ttl, false), Body: &dnsmessage.PTRResource{PTR: r.instName}}
}

// answer returns the records answering q and the additional records that complete them.
func (r *records) answer(q dnsmessage.Question, ips []net.IP) (answers, extra []dnsmessage.Resource) {
	return r.answerTTL(q, ips, 0, true)
}

func (r *records) answerTTL(q dnsmessage.Question, ips []net.IP, maxTTL uint32, unique bool) (answers, extra []dnsmessage.Resource) {
	ttl := func(t uint32) uint32 {
		if maxTTL > 0 && t > maxTTL {
			return maxTTL
		}
		return t
	}
	is := func(t dnsmessage.Type) bool { return q.Type == t || q.Type == dnsmessage.TypeALL }
	switch {
	case same(q.Name, dnsmessage.MustNewName(servicesName)) && is(dnsmessage.TypePTR):
		answers = append(answers, dnsmessage.Resource{Header: header(q.Name, dnsmessage.TypePTR, ttl(serviceTTL), false),
			Body: &dnsmessage.PTRResource{PTR: r.typeName}})
	case same(q.Name, r.typeName) && is(dnsmessage.TypePTR):
		answers = append(answers, r.ptr(ttl(serviceTTL)))
		extra = append(extra, r.srv(ttl(hostTTL), unique), r.txt(ttl(serviceTTL), unique))
		extra = append(extra, r.addresses(ips, ttl(hostTTL), unique)...)
	case same(q.Name, r.instName):
		if is(dnsmessage.TypeSRV) {
			answers = append(answers, r.srv(ttl(hostTTL), unique))
			extra = append(extra, r.addresses(ips, ttl(hostTTL), unique)...)
		}
		if is(dnsmessage.TypeTXT) {
			answers = append(answers, r.txt(ttl(serviceTTL), unique))
		}
	case same(q.Name, r.hostName) && is(dnsmessage.TypeA):
		answers = append(answers, r.addresses(ips, ttl(hostTTL), unique)...)
	}
	return answers, extra
}

// respond builds the reply to one received packet. unicast means "send it back to the sender";
// otherwise it goes to the multicast group. ok is false when there is nothing to answer.
func (r *records) respond(packet []byte, fromPort int, ips []net.IP) (reply []byte, unicast, ok bool) {
	var p dnsmessage.Parser
	h, err := p.Start(packet)
	if err != nil || h.Response || h.OpCode != 0 {
		return nil, false, false
	}
	questions, err := p.AllQuestions()
	if err != nil {
		return nil, false, false
	}
	legacy := fromPort != mdnsPort
	var maxTTL uint32
	if legacy {
		maxTTL = legacyMaxTTL
	}
	var answers, extra []dnsmessage.Resource
	var echoed []dnsmessage.Question
	for _, q := range questions {
		if q.Class&dnsClassUnicast != 0 {
			unicast = true
		}
		q.Class &^= dnsClassUnicast
		a, e := r.answerTTL(q, ips, maxTTL, !legacy)
		if len(a) > 0 {
			echoed = append(echoed, q)
		}
		answers = append(answers, a...)
		extra = append(extra, e...)
	}
	if len(answers) == 0 {
		return nil, false, false
	}
	hdr := dnsmessage.Header{Response: true, Authoritative: true}
	if legacy {
		hdr.ID = h.ID
	}
	out, err := build(hdr, map[bool][]dnsmessage.Question{true: echoed}[legacy], answers, extra)
	if err != nil {
		return nil, false, false
	}
	return out, unicast || legacy, true
}

func build(h dnsmessage.Header, questions []dnsmessage.Question, answers, extra []dnsmessage.Resource) ([]byte, error) {
	b := dnsmessage.NewBuilder(make([]byte, 0, 512), h)
	b.EnableCompression()
	if err := b.StartQuestions(); err != nil {
		return nil, err
	}
	for _, q := range questions {
		if err := b.Question(q); err != nil {
			return nil, err
		}
	}
	add := func(rs []dnsmessage.Resource) error {
		for _, r := range rs {
			var err error
			switch body := r.Body.(type) {
			case *dnsmessage.PTRResource:
				err = b.PTRResource(r.Header, *body)
			case *dnsmessage.SRVResource:
				err = b.SRVResource(r.Header, *body)
			case *dnsmessage.TXTResource:
				err = b.TXTResource(r.Header, *body)
			case *dnsmessage.AResource:
				err = b.AResource(r.Header, *body)
			}
			if err != nil {
				return err
			}
		}
		return nil
	}
	if err := b.StartAnswers(); err != nil {
		return nil, err
	}
	if err := add(answers); err != nil {
		return nil, err
	}
	if err := b.StartAdditionals(); err != nil {
		return nil, err
	}
	if err := add(dedupe(answers, extra)); err != nil {
		return nil, err
	}
	return b.Finish()
}

func dedupe(answers, extra []dnsmessage.Resource) []dnsmessage.Resource {
	seen := map[string]bool{}
	key := func(r dnsmessage.Resource) string {
		return strings.ToLower(r.Header.Name.String()) + r.Header.Type.String() + r.Body.GoString()
	}
	for _, a := range answers {
		seen[key(a)] = true
	}
	var out []dnsmessage.Resource
	for _, e := range extra {
		if !seen[key(e)] {
			seen[key(e)] = true
			out = append(out, e)
		}
	}
	return out
}

// Advertise registers s until the returned stop function runs, through the system's Bonjour
// daemon when available and the built-in responder otherwise.
func Advertise(s Service) (stop func(), err error) {
	if SystemAvailable() {
		return AdvertiseSystem(s)
	}
	return AdvertiseBuiltin(s)
}

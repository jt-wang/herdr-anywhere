package connectmdns

import (
	"bufio"
	"context"
	"errors"
	"net"
	"os/exec"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"time"
)

// SystemBrowseAvailable reports whether BrowseSystem can run: dns-sd on macOS, avahi-browse on Linux.
func SystemBrowseAvailable() bool {
	switch runtime.GOOS {
	case "darwin":
		return SystemAvailable()
	case "linux":
		_, err := exec.LookPath("avahi-browse")
		return err == nil
	}
	return false
}

// BrowseSystem browses through the OS daemon's command-line tool.
func BrowseSystem(ctx context.Context, typ string) (<-chan Entry, error) {
	if !ValidType(typ) {
		return nil, errors.New("connectmdns: service type must look like _name._tcp")
	}
	switch runtime.GOOS {
	case "darwin":
		return browseDNSSD(ctx, typ)
	case "linux":
		return browseAvahi(ctx, typ)
	}
	return nil, errors.New("connectmdns: no system browser on " + runtime.GOOS)
}

// dns-sd -B: " 4:17:35.529  Add        3  26 local.               _tailpocket-x._tcp. tailpocket probe"
var browseLine = regexp.MustCompile(`^\s*\S+\s+(Add|Rmv)\s+\S+\s+\d+\s+\S+\s+\S+\s+(.+)$`)

// dns-sd -L: "... tailpocket\032probe._x._tcp.local. can be reached at host.local.:40999 (interface 15) Flags: 1"
var resolveLine = regexp.MustCompile(`can be reached at (\S+):(\d+) \(interface (\d+)\)`)

// dns-sd -G: " 4:19:23.242  Add  40000003      26  host.local.     192.168.1.2       4500"
var addrLine = regexp.MustCompile(`^\s*\S+\s+Add\s+\S+\s+(\d+)\s+\S+\s+(\d+\.\d+\.\d+\.\d+)\s`)

func browseDNSSD(ctx context.Context, typ string) (<-chan Entry, error) {
	cmd := exec.CommandContext(ctx, darwinDNSSD, "-B", typ, "local")
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	out := make(chan Entry, 8)
	var wg sync.WaitGroup
	go func() {
		seen := map[string]bool{}
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			m := browseLine.FindStringSubmatch(sc.Text())
			if m == nil {
				continue
			}
			instance := strings.TrimRight(m[2], " ")
			if m[1] == "Rmv" {
				delete(seen, instance)
				continue
			}
			if seen[instance] {
				continue
			}
			seen[instance] = true
			wg.Add(1)
			go func() {
				defer wg.Done()
				if e, ok := resolveDNSSD(ctx, instance, typ); ok {
					select {
					case out <- e:
					case <-ctx.Done():
					}
				}
			}()
		}
		_ = cmd.Wait()
		wg.Wait()
		close(out)
	}()
	return out, nil
}

// resolveDNSSD runs dns-sd -L for host, port and TXT, then dns-sd -G v4 for the addresses.
func resolveDNSSD(ctx context.Context, instance, typ string) (Entry, bool) {
	rctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	lines, wait := run(rctx, darwinDNSSD, "-L", instance, typ, "local")
	e := Entry{Instance: instance, Type: typ, TXT: map[string]string{}}
	ifindex := ""
	for line := range lines {
		if m := resolveLine.FindStringSubmatch(line); m != nil {
			e.Host = strings.ToLower(m[1])
			e.Port, _ = strconv.Atoi(m[2])
			ifindex = m[3]
			if txt, ok := <-lines; ok {
				e.TXT = parseTXT(txt)
			}
			break
		}
	}
	cancel()
	wait()
	if e.Port == 0 {
		return Entry{}, false
	}
	actx, acancel := context.WithTimeout(ctx, 2*time.Second)
	defer acancel()
	lines, wait = run(actx, darwinDNSSD, "-G", "v4", e.Host)
	var first, rest []net.IP
	for line := range lines {
		m := addrLine.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		ip := net.ParseIP(m[2]).To4()
		if ip == nil || ip.IsLoopback() || containsIP(first, ip) || containsIP(rest, ip) {
			continue
		}
		if m[1] == ifindex {
			first = append(first, ip)
			acancel() // the resolving interface answered; that is the address to use
		} else {
			rest = append(rest, ip)
		}
	}
	wait()
	e.IPv4 = append(first, rest...)
	return e, len(e.IPv4) > 0
}

// run streams a command's stdout lines until ctx ends.
func run(ctx context.Context, name string, args ...string) (<-chan string, func()) {
	cmd := exec.CommandContext(ctx, name, args...)
	stdout, err := cmd.StdoutPipe()
	lines := make(chan string, 16)
	if err != nil || cmd.Start() != nil {
		close(lines)
		return lines, func() {}
	}
	done := make(chan struct{})
	go func() {
		defer close(done)
		defer close(lines)
		sc := bufio.NewScanner(stdout)
		for sc.Scan() {
			select {
			case lines <- sc.Text():
			case <-ctx.Done():
				return
			}
		}
	}()
	return lines, func() {
		<-done
		_ = cmd.Wait()
	}
}

// parseTXT reads dns-sd's TXT line: space-separated, "\ " for a literal space, "\DDD" for bytes.
func parseTXT(line string) map[string]string {
	out := map[string]string{}
	var cur strings.Builder
	flush := func() {
		if cur.Len() == 0 {
			return
		}
		k, v, _ := strings.Cut(cur.String(), "=")
		out[strings.ToLower(k)] = v
		cur.Reset()
	}
	line = strings.TrimLeft(line, " ")
	for i := 0; i < len(line); i++ {
		c := line[i]
		switch {
		case c == '\\' && i+3 < len(line) && isDigit(line[i+1]) && isDigit(line[i+2]) && isDigit(line[i+3]):
			n, _ := strconv.Atoi(line[i+1 : i+4])
			cur.WriteByte(byte(n))
			i += 3
		case c == '\\' && i+1 < len(line):
			cur.WriteByte(line[i+1])
			i++
		case c == ' ':
			flush()
		default:
			cur.WriteByte(c)
		}
	}
	flush()
	return out
}

func isDigit(c byte) bool { return c >= '0' && c <= '9' }

// avahi-browse -p -r: "=;eth0;IPv4;name;_x._tcp;local;host.local;192.168.1.2;40999;"v=1""
func browseAvahi(ctx context.Context, typ string) (<-chan Entry, error) {
	lines, wait := run(ctx, "avahi-browse", "-p", "-r", typ)
	out := make(chan Entry, 8)
	go func() {
		defer close(out)
		defer wait()
		sent := map[string]bool{}
		for line := range lines {
			f := strings.Split(line, ";")
			if len(f) < 10 || f[0] != "=" || f[2] != "IPv4" {
				continue
			}
			ip := net.ParseIP(f[7]).To4()
			port, _ := strconv.Atoi(f[8])
			if ip == nil || port == 0 {
				continue
			}
			e := Entry{Instance: avahiUnescape(f[3]), Type: typ, Host: strings.ToLower(f[6]), Port: port, IPv4: []net.IP{ip}, TXT: map[string]string{}}
			for _, t := range strings.Fields(strings.Join(f[9:], ";")) {
				k, v, _ := strings.Cut(strings.Trim(t, `"`), "=")
				e.TXT[strings.ToLower(k)] = v
			}
			if sent[e.Instance+e.Addr()] {
				continue
			}
			sent[e.Instance+e.Addr()] = true
			select {
			case out <- e:
			case <-ctx.Done():
				return
			}
		}
	}()
	return out, nil
}

func avahiUnescape(s string) string {
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		if s[i] == '\\' && i+3 < len(s) && isDigit(s[i+1]) && isDigit(s[i+2]) && isDigit(s[i+3]) {
			n, _ := strconv.Atoi(s[i+1 : i+4])
			b.WriteByte(byte(n))
			i += 3
			continue
		}
		b.WriteByte(s[i])
	}
	return b.String()
}

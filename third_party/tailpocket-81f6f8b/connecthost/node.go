package connecthost

import (
	"context"
	"errors"
	"net"
	"strings"
	"sync"

	"github.com/tailscale/tailcat"
	"tailscale.com/tailcfg"
	"tailscale.com/types/key"
	"tailscale.com/wgengine/filter"
)

type nodeSpec struct {
	key        key.NodePrivate
	psk        tailcat.PresharedKey
	region     *tailcfg.DERPRegion
	allowed    []key.NodePublic
	port       uint16
	control    uint16 // a second port the node serves, 0 for none
	onTCP      func(uint16) func(net.Conn)
	derpMapURL string
}

type node interface {
	Address() string
	Region() *tailcfg.DERPRegion
	Allow(key.NodePublic)
	Close() error
}

type starter func(context.Context, nodeSpec) (node, error)

type tailNode struct {
	server *tailcat.Server
	addr   string
	region *tailcfg.DERPRegion
}

func (n *tailNode) Address() string             { return n.addr }
func (n *tailNode) Region() *tailcfg.DERPRegion { return n.region }
func (n *tailNode) Allow(k key.NodePublic)      { n.server.AddAllowedClient(k) }
func (n *tailNode) Close() error                { return n.server.Close() }

func realStart(ctx context.Context, s nodeSpec) (node, error) {
	if len(s.allowed) == 0 {
		return nil, errors.New("connecthost: refusing to start a node that admits every client")
	}
	server := &tailcat.Server{
		Key:            s.key,
		PresharedKey:   s.psk,
		Region:         s.region,
		DERPMapURL:     s.derpMapURL,
		AllowedClients: s.allowed,
		Logf:           func(string, ...any) {},
		OnTCP:          s.onTCP,
		ServedTCPPorts: servedPorts(s),
	}
	errc := make(chan error, 1)
	var once sync.Once
	go func() { errc <- server.Start() }()
	select {
	case <-ctx.Done():
		go func() {
			if <-errc == nil {
				once.Do(func() { _ = server.Close() })
			}
		}()
		return nil, ctx.Err()
	case err := <-errc:
		if err != nil {
			return nil, err
		}
	}
	addr := string(server.TailcatAddr())
	info, err := tailcat.ParseAddr(tailcat.Addr(addr))
	if err != nil || !strings.HasPrefix(addr, "tc") || len(info.Region) != 1 || info.Region[0] == nil {
		_ = server.Close()
		return nil, errors.New("connecthost: node started without a usable address")
	}
	return &tailNode{server: server, addr: addr, region: info.Region[0]}, nil
}

// chanListener turns handed-over connections into a net.Listener.
type chanListener struct {
	conns chan net.Conn
	done  chan struct{}
	once  sync.Once
}

func newChanListener() *chanListener {
	return &chanListener{conns: make(chan net.Conn), done: make(chan struct{})}
}

// offer blocks until Accept takes c or the listener closes (then c is closed).
func (l *chanListener) offer(c net.Conn) {
	select {
	case l.conns <- c:
	case <-l.done:
		_ = c.Close()
	}
}

func (l *chanListener) Accept() (net.Conn, error) {
	for {
		select {
		case c := <-l.conns:
			if pc, ok := c.(*Conn); ok && pc.revoked.Load() {
				continue // closed by Revoke before the app took it
			}
			return c, nil
		case <-l.done:
			return nil, net.ErrClosed
		}
	}
}

func (l *chanListener) Close() error {
	l.once.Do(func() { close(l.done) })
	return nil
}

func (l *chanListener) Addr() net.Addr { return tailcatAddr{} }

type tailcatAddr struct{}

func (tailcatAddr) Network() string { return "tailcat" }
func (tailcatAddr) String() string  { return "tailcat" }

func servedPorts(s nodeSpec) []filter.PortRange {
	out := []filter.PortRange{{First: s.port, Last: s.port}}
	if s.control != 0 {
		out = append(out, filter.PortRange{First: s.control, Last: s.control})
	}
	return out
}

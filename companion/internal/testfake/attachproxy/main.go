// attachproxy is local native-E2E tooling, not a production server. It delays
// terminal.attach replies without delaying early terminal output or events.
package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/binary"
	"encoding/json"
	"errors"
	"flag"
	"io"
	"log"
	"net"
	"net/http"
	"net/http/httputil"
	"net/url"
	"sync"
	"sync/atomic"
	"time"

	"github.com/coder/websocket"
)

const (
	maxPayload = 16 << 20 // app/src/hc/frames.ts
	maxEntries = 1024
	maxHeld    = 32 << 20
)

type counters struct {
	AttachRequests    atomic.Uint64
	HeldResponses     atomic.Uint64
	ReleasedResponses atomic.Uint64
	ClientHeldCloses  atomic.Uint64
}

type proxy struct {
	mu       sync.Mutex
	hold     bool
	conns    map[*bridge]struct{}
	upstream *url.URL
	http     *httputil.ReverseProxy
	counts   counters
}

func newProxy(upstream *url.URL) *proxy {
	rp := httputil.NewSingleHostReverseProxy(upstream)
	// Errors can contain credential-bearing URLs. Never log them.
	rp.ErrorLog = log.New(io.Discard, "", 0)
	rp.ErrorHandler = func(w http.ResponseWriter, _ *http.Request, _ error) {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
	}
	return &proxy{upstream: upstream, http: rp, conns: make(map[*bridge]struct{})}
}

func (p *proxy) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	switch r.URL.Path {
	case "/stats":
		if r.Method != http.MethodGet {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		p.mu.Lock()
		active := len(p.conns)
		p.mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		w.Header().Set("Cache-Control", "no-store")
		_ = json.NewEncoder(w).Encode(struct {
			AttachRequests    uint64 `json:"attach_requests"`
			HeldResponses     uint64 `json:"held_responses"`
			ReleasedResponses uint64 `json:"released_responses"`
			ClientHeldCloses  uint64 `json:"client_held_closes"`
			ActiveConnections int    `json:"active_connections"`
		}{p.counts.AttachRequests.Load(), p.counts.HeldResponses.Load(),
			p.counts.ReleasedResponses.Load(), p.counts.ClientHeldCloses.Load(), active})
	case "/fault":
		if r.Method != http.MethodGet && r.Method != http.MethodPost {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		mode := r.URL.Query().Get("mode")
		if mode != "hold" && mode != "pass" && mode != "drop" {
			http.Error(w, "mode must be hold, pass, or drop", http.StatusBadRequest)
			return
		}
		p.mu.Lock()
		if mode == "drop" {
			for c := range p.conns {
				c.stop()
			}
		} else {
			p.hold = mode == "hold"
			for c := range p.conns {
				c.mu.Lock()
				c.hold = p.hold
				if !c.hold {
					for _, frame := range c.held {
						if c.write(c.client, frame) != nil {
							c.stop()
							break
						}
						p.counts.ReleasedResponses.Add(1)
					}
					c.held = nil
					c.heldBytes = 0
				}
				c.mu.Unlock()
			}
		}
		p.mu.Unlock()
		w.Header().Set("Cache-Control", "no-store")
		w.WriteHeader(http.StatusNoContent)
	case "/hc":
		p.hc(w, r)
	default:
		p.http.ServeHTTP(w, r)
	}
}

type bridge struct {
	p                *proxy
	client, upstream *websocket.Conn
	ctx              context.Context
	cancel           context.CancelFunc
	b64              bool
	mu               sync.Mutex // also serializes release and every downstream write
	hold             bool
	pending          map[string]bool
	heldChannels     map[uint32]bool
	held             [][]byte
	heldBytes        int
}

func (c *bridge) stop() {
	c.cancel()
	_ = c.client.CloseNow()
	_ = c.upstream.CloseNow()
}

func (c *bridge) write(dst *websocket.Conn, b []byte) error {
	typ := websocket.MessageBinary
	if c.b64 {
		typ, b = websocket.MessageText, []byte(base64.StdEncoding.EncodeToString(b))
	}
	ctx, cancel := context.WithTimeout(c.ctx, 5*time.Second)
	defer cancel()
	return dst.Write(ctx, typ, b)
}

func (p *proxy) hc(w http.ResponseWriter, r *http.Request) {
	u := *p.upstream
	u.Path = r.URL.Path
	u.RawQuery = r.URL.RawQuery
	if u.Scheme == "https" {
		u.Scheme = "wss"
	} else {
		u.Scheme = "ws"
	}
	// No Origin: Companion checks sameOrigin against its own host. Forward only
	// authentication headers, not hop-by-hop WebSocket handshake headers.
	headers := make(http.Header)
	for _, key := range []string{"Authorization", "Cookie"} {
		headers[key] = r.Header.Values(key)
	}
	up, _, err := websocket.Dial(r.Context(), u.String(), &websocket.DialOptions{HTTPHeader: headers})
	if err != nil {
		http.Error(w, "upstream unavailable", http.StatusBadGateway)
		return
	}
	client, err := websocket.Accept(w, r, &websocket.AcceptOptions{InsecureSkipVerify: true})
	if err != nil {
		_ = up.CloseNow()
		return
	}
	ctx, cancel := context.WithCancel(r.Context())
	c := &bridge{p: p, client: client, upstream: up, ctx: ctx, cancel: cancel,
		b64: r.URL.Query().Get("enc") == "b64", pending: make(map[string]bool), heldChannels: make(map[uint32]bool)}
	client.SetReadLimit(24 << 20)
	up.SetReadLimit(24 << 20)
	p.mu.Lock()
	c.hold = p.hold
	p.conns[c] = struct{}{}
	p.mu.Unlock()
	defer func() {
		c.stop()
		p.mu.Lock()
		delete(p.conns, c)
		p.mu.Unlock()
	}()
	done := make(chan struct{})
	go func() {
		defer close(done)
		c.pump(client, false)
		c.stop()
	}()
	c.pump(up, true)
	c.stop()
	<-done
}

// Each direction starts with one newline-terminated hello, then a byte stream
// of frames. WebSocket message boundaries need not coincide with either.
func (c *bridge) pump(src *websocket.Conn, downstream bool) {
	var buf []byte
	hello := true
	for {
		typ, msg, err := src.Read(c.ctx)
		if err != nil {
			return
		}
		if c.b64 {
			if typ != websocket.MessageText {
				return
			}
			msg, err = base64.StdEncoding.DecodeString(string(msg))
		} else if typ != websocket.MessageBinary {
			return
		}
		if err != nil {
			return
		}
		buf = append(buf, msg...)
		if hello {
			n := bytes.IndexByte(buf, '\n')
			if n < 0 {
				if len(buf) > 1<<20 {
					return
				}
				continue
			}
			if n > 1<<20 {
				return
			}
			if c.forward(buf[:n+1], downstream, false) != nil {
				return
			}
			buf, hello = buf[n+1:], false
		}
		for len(buf) >= 9 {
			n := int(binary.LittleEndian.Uint32(buf[5:9]))
			if n > maxPayload {
				return
			}
			if len(buf) < 9+n {
				break
			}
			if c.forward(buf[:9+n], downstream, true) != nil {
				return
			}
			buf = buf[9+n:]
		}
		// Do not retain a large backing array for a tiny partial frame.
		buf = bytes.Clone(buf)
	}
}

func (c *bridge) forward(frame []byte, downstream, framed bool) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if framed {
		ch, kind := binary.LittleEndian.Uint32(frame), frame[4]
		if !downstream && kind == 2 && c.heldChannels[ch] {
			// Keep the bounded channel set after release, so late CLOSEs (and
			// accidental duplicates) remain visible until this socket ends.
			c.p.counts.ClientHeldCloses.Add(1)
		}
		if ch == 0 && kind == 0 {
			// The JSON-RPC envelope and attach result are from client.ts.
			var rpc struct {
				ID     json.RawMessage `json:"id"`
				Method string          `json:"method"`
				Result struct {
					Channel uint32 `json:"channel"`
				} `json:"result"`
			}
			if json.Unmarshal(frame[9:], &rpc) == nil && len(rpc.ID) != 0 {
				id := string(rpc.ID)
				if !downstream && rpc.Method == "terminal.attach" {
					if len(c.pending) >= maxEntries || len(id) > 128 {
						return errors.New("pending limit")
					}
					c.pending[id] = true
					c.p.counts.AttachRequests.Add(1)
				}
				if downstream && rpc.Method == "" && c.pending[id] {
					delete(c.pending, id)
					if c.hold {
						if len(c.held) >= maxEntries || len(c.heldChannels) >= maxEntries || c.heldBytes+len(frame) > maxHeld {
							return errors.New("held limit")
						}
						c.held = append(c.held, bytes.Clone(frame))
						c.heldBytes += len(frame)
						if rpc.Result.Channel != 0 {
							c.heldChannels[rpc.Result.Channel] = true
						}
						c.p.counts.HeldResponses.Add(1)
						return nil
					}
				}
			}
		}
	}
	if downstream {
		return c.write(c.client, frame)
	}
	return c.write(c.upstream, frame)
}

func main() {
	listen := flag.String("listen", "127.0.0.1:7992", "local test listener")
	upstream := flag.String("upstream", "http://127.0.0.1:7991", "Companion HTTP origin")
	flag.Parse()
	host, _, err := net.SplitHostPort(*listen)
	if err != nil || net.ParseIP(host) == nil || !net.ParseIP(host).IsLoopback() {
		log.Fatal("listen must be a loopback IP and port")
	}
	u, err := url.Parse(*upstream)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" || u.User != nil || u.RawQuery != "" || u.Fragment != "" || (u.Path != "" && u.Path != "/") {
		log.Fatal("upstream must be an HTTP origin without credentials, path, query, or fragment")
	}
	s := &http.Server{Addr: *listen, Handler: newProxy(u), ReadHeaderTimeout: 5 * time.Second,
		ErrorLog: log.New(io.Discard, "", 0)}
	log.Fatal(s.ListenAndServe())
}

package connectphone

import (
	"context"
	"io"
	"net"
	"sync"
)

// Forward is a loopback listener whose connections continue to the host.
type Forward struct {
	ln     net.Listener
	ctx    context.Context
	cancel context.CancelFunc
	mu     sync.Mutex
	closed bool
	live   map[net.Conn]struct{}
	wg     sync.WaitGroup
	once   sync.Once
}

func startForward(dial func(context.Context) (net.Conn, error)) (*Forward, error) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(context.Background())
	f := &Forward{ln: ln, ctx: ctx, cancel: cancel, live: map[net.Conn]struct{}{}}
	f.wg.Add(1)
	go func() {
		defer f.wg.Done()
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			if !f.track(c) {
				return
			}
			f.wg.Add(1)
			go f.bridge(c, dial)
		}
	}()
	return f, nil
}

// Addr is "127.0.0.1:<port>".
func (f *Forward) Addr() string { return f.ln.Addr().String() }

// Port is the loopback port.
func (f *Forward) Port() int { return f.ln.Addr().(*net.TCPAddr).Port }

func (f *Forward) track(c net.Conn) bool {
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.closed {
		_ = c.Close()
		return false
	}
	f.live[c] = struct{}{}
	return true
}

func (f *Forward) untrack(c net.Conn) {
	_ = c.Close()
	f.mu.Lock()
	delete(f.live, c)
	f.mu.Unlock()
}

func (f *Forward) bridge(local net.Conn, dial func(context.Context) (net.Conn, error)) {
	defer f.wg.Done()
	defer f.untrack(local)
	remote, err := dial(f.ctx)
	if err != nil {
		return // remote may be a nil pointer inside a non-nil net.Conn; never touch it
	}
	if !f.track(remote) {
		_ = remote.Close()
		return
	}
	defer f.untrack(remote)
	var copying sync.WaitGroup
	copying.Add(2)
	pipe := func(dst, src net.Conn) {
		defer copying.Done()
		_, _ = io.Copy(dst, src)
		if cw, ok := dst.(interface{ CloseWrite() error }); ok {
			_ = cw.CloseWrite()
		} else {
			_ = dst.Close()
		}
	}
	go pipe(remote, local)
	go pipe(local, remote)
	copying.Wait()
}

// Close stops listening and cuts every open connection.
func (f *Forward) Close() {
	f.once.Do(func() {
		f.cancel()
		_ = f.ln.Close()
		f.mu.Lock()
		f.closed = true
		for c := range f.live {
			_ = c.Close()
		}
		f.mu.Unlock()
		f.wg.Wait()
	})
}

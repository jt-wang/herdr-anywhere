package connectsecure

import (
	"bytes"
	"encoding/json"
	"fmt"
	"sync"
)

// Control limits: what goes over it is short. A peer over either limit loses
// the session.
const (
	maxControlLine  = 4 << 10  // one line, without its newline
	maxControlBytes = 64 << 10 // lines received and not yet read by Recv, together
)

// Control is stream 0: one JSON object per line. It has no window; the byte
// limits above bound it instead. Ping and pong are answered by the session and
// never come out of Recv.
type Control struct {
	s   *Session
	smu sync.Mutex // one message at a time, even when it spans frames

	mu      sync.Mutex
	partial []byte
	queue   []queued
	qbytes  int
	sig     chan struct{}
}

type queued struct {
	m    map[string]any
	size int
}

// Send writes msg as one JSON line.
func (c *Control) Send(msg any) error {
	b, err := json.Marshal(msg)
	if err != nil {
		return err
	}
	if len(b) > maxControlLine {
		return fmt.Errorf("secure: control message of %d bytes is over %d", len(b), maxControlLine)
	}
	b = append(b, '\n')
	c.smu.Lock()
	defer c.smu.Unlock()
	for len(b) > 0 {
		n := min(len(b), maxFramePayload)
		if err := c.s.writeFrame(0, frameData, b[:n]); err != nil {
			return err
		}
		b = b[n:]
	}
	return nil
}

// Recv waits for the next control message. After the session ends it returns
// what is still queued, then the session's error.
func (c *Control) Recv() (map[string]any, error) {
	for {
		c.mu.Lock()
		if len(c.queue) > 0 {
			q := c.queue[0]
			c.queue = c.queue[1:]
			c.qbytes -= q.size
			c.mu.Unlock()
			return q.m, nil
		}
		c.mu.Unlock()
		if c.s.isClosed() {
			return nil, c.s.Err()
		}
		select {
		case <-c.sig:
		case <-c.s.done:
		}
	}
}

// feed takes bytes from a stream-0 frame. Called only by the reader.
func (c *Control) feed(p []byte) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.partial = append(c.partial, p...)
	for {
		i := bytes.IndexByte(c.partial, '\n')
		if i < 0 {
			break
		}
		line := c.partial[:i]
		if len(line) > maxControlLine {
			return ErrProtocol
		}
		m, err := parseLine(line)
		c.partial = c.partial[i+1:]
		if err != nil {
			return ErrProtocol
		}
		switch m["type"] {
		case "ping":
			signal(c.s.pong)
		case "pong":
		default:
			if c.qbytes+len(line) > maxControlBytes {
				return ErrProtocol
			}
			c.queue = append(c.queue, queued{m, len(line)})
			c.qbytes += len(line)
			signal(c.sig)
		}
	}
	if len(c.partial) > maxControlLine {
		return ErrProtocol
	}
	if len(c.partial) == 0 {
		c.partial = nil
	}
	return nil
}

func parseLine(line []byte) (map[string]any, error) {
	var m map[string]any
	if err := json.Unmarshal(line, &m); err != nil {
		return nil, err
	}
	if m == nil {
		return nil, ErrProtocol
	}
	return m, nil
}

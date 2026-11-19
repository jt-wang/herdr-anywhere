package connectrelay

import (
	"net/netip"
	"sync"
	"time"
)

// AddrKey is the source address limits count against: an IPv4 address as
// it is, an IPv6 address by its /64 (one home or phone usually gets a whole
// /64, so counting single IPv6 addresses would not limit anyone).
func AddrKey(a netip.Addr) netip.Addr {
	a = a.Unmap()
	if a.Is6() {
		p, err := a.Prefix(64)
		if err == nil {
			return p.Addr()
		}
	}
	return a
}

// rateLimiter allows at most n events per key in any window: a sliding
// window that keeps the times of the last n events.
type rateLimiter struct {
	n      int
	window time.Duration
	now    func() time.Time

	mu        sync.Mutex
	keys      map[netip.Addr]*ring
	lastSweep time.Time
}

type ring struct {
	at   []int64 // unix nanoseconds; once full, the oldest is at next
	next int
}

// maxKeys bounds memory. Past it, keys whose last event left the window are
// swept (at most once a second); if none can go, new keys are refused.
const maxKeys = 1 << 16

func newRateLimiter(n int, window time.Duration, now func() time.Time) *rateLimiter {
	return &rateLimiter{n: n, window: window, now: now, keys: map[netip.Addr]*ring{}}
}

func (r *ring) newest() int64 {
	i := r.next - 1
	if i < 0 {
		i = len(r.at) - 1
	}
	return r.at[i]
}

func (l *rateLimiter) allow(a netip.Addr) bool {
	k := AddrKey(a)
	t := l.now()
	now := t.UnixNano()
	cut := now - l.window.Nanoseconds()
	l.mu.Lock()
	defer l.mu.Unlock()
	r := l.keys[k]
	if r == nil {
		if len(l.keys) >= maxKeys {
			if t.Sub(l.lastSweep) >= time.Second {
				l.lastSweep = t
				for key, v := range l.keys {
					if v.newest() <= cut {
						delete(l.keys, key)
					}
				}
			}
			if len(l.keys) >= maxKeys {
				return false
			}
		}
		r = &ring{at: make([]int64, 0, l.n)}
		l.keys[k] = r
	}
	if len(r.at) < l.n {
		r.at = append(r.at, now)
		r.next = len(r.at) % l.n
		return true
	}
	// Full: the oldest of the last n must have left the window.
	if r.at[r.next] > cut {
		return false
	}
	r.at[r.next] = now
	r.next = (r.next + 1) % l.n
	return true
}

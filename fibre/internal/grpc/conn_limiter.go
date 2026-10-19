package grpc

import (
	"net"
	"sync"
	"sync/atomic"
)

// ConnLimiter fail-fast rejects over-cap connections (fixed global fd-safety cap
// plus a per-IP cap), closing them rather than parking them in the backlog.
type ConnLimiter struct {
	globalMax int

	mu   sync.Mutex
	open int

	perIP          *keyedCounter
	rejectedGlobal atomic.Int64
	rejectedPerIP  atomic.Int64
}

func newConnLimiter(globalMax, perIPMax int) *ConnLimiter {
	return &ConnLimiter{globalMax: globalMax, perIP: newKeyedCounter(perIPMax)}
}

// OpenConns returns the open admitted connections.
func (cl *ConnLimiter) OpenConns() int64 {
	cl.mu.Lock()
	defer cl.mu.Unlock()
	return int64(cl.open)
}

// RejectedGlobal returns connections rejected by the global cap.
func (cl *ConnLimiter) RejectedGlobal() int64 { return cl.rejectedGlobal.Load() }

// RejectedPerIP returns connections rejected by the per-IP cap.
func (cl *ConnLimiter) RejectedPerIP() int64 { return cl.rejectedPerIP.Load() }

func (cl *ConnLimiter) acquire(key string) bool {
	cl.mu.Lock()
	if cl.globalMax > 0 && cl.open >= cl.globalMax {
		cl.mu.Unlock()
		cl.rejectedGlobal.Add(1)
		return false
	}
	cl.open++
	cl.mu.Unlock()

	if !cl.perIP.tryAcquire(key) {
		cl.mu.Lock()
		cl.open--
		cl.mu.Unlock()
		cl.rejectedPerIP.Add(1)
		return false
	}
	return true
}

func (cl *ConnLimiter) release(key string) {
	cl.mu.Lock()
	if cl.open > 0 {
		cl.open--
	}
	cl.mu.Unlock()
	cl.perIP.release(key)
}

// limitedListener fail-fast rejects over-cap connections.
type limitedListener struct {
	net.Listener
	limiter *ConnLimiter
}

func newLimitedListener(inner net.Listener, limiter *ConnLimiter) *limitedListener {
	return &limitedListener{Listener: inner, limiter: limiter}
}

func (l *limitedListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		key := connKey(c.RemoteAddr())
		if !l.limiter.acquire(key) {
			_ = c.Close() // fail fast: close rather than park in the backlog
			continue
		}
		return &limitedConn{Conn: c, limiter: l.limiter, key: key}, nil
	}
}

// limitedConn releases its slot on Close (idempotent; grpc-go may close twice).
type limitedConn struct {
	net.Conn
	limiter *ConnLimiter
	key     string
	closed  atomic.Bool
}

func (c *limitedConn) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		c.limiter.release(c.key)
	}
	return c.Conn.Close()
}

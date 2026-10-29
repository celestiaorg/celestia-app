package grpc

import (
	"net"
	"sync/atomic"
)

// limitedListener closes over-cap connections at once rather than parking them
// in the backlog: a fixed global fd-safety cap plus a per-IP cap.
type limitedListener struct {
	net.Listener
	globalMax int64
	open      atomic.Int64
	perIP     *keyedCounter
}

func newLimitedListener(inner net.Listener, globalMax int64, perIPMax int) *limitedListener {
	return &limitedListener{Listener: inner, globalMax: globalMax, perIP: newKeyedCounter(perIPMax)}
}

func (l *limitedListener) Accept() (net.Conn, error) {
	for {
		c, err := l.Listener.Accept()
		if err != nil {
			return nil, err
		}
		key := connKey(c.RemoteAddr())
		if !l.acquire(key) {
			_ = c.Close()
			continue
		}
		return &limitedConn{Conn: c, l: l, key: key}, nil
	}
}

func (l *limitedListener) acquire(key string) bool {
	if l.open.Add(1) > l.globalMax {
		l.open.Add(-1)
		return false
	}
	if !l.perIP.tryAcquire(key) {
		l.open.Add(-1)
		return false
	}
	return true
}

func (l *limitedListener) release(key string) {
	l.open.Add(-1)
	l.perIP.release(key)
}

// limitedConn frees its slot on the first Close; grpc-go may close twice.
type limitedConn struct {
	net.Conn
	l      *limitedListener
	key    string
	closed atomic.Bool
}

func (c *limitedConn) Close() error {
	if c.closed.CompareAndSwap(false, true) {
		c.l.release(c.key)
	}
	return c.Conn.Close()
}

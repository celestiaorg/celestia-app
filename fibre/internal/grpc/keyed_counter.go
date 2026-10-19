package grpc

import (
	"net"
	"sync"
)

// keyedCounter bounds how many things a key may hold at once (0 = unlimited).
type keyedCounter struct {
	mu  sync.Mutex
	max int
	m   map[string]int
}

func newKeyedCounter(max int) *keyedCounter {
	return &keyedCounter{max: max, m: make(map[string]int)}
}

// tryAcquire increments key's count if under the cap. An empty key is unlimited.
func (c *keyedCounter) tryAcquire(key string) bool {
	if c.max <= 0 || key == "" {
		return true
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.m[key] >= c.max {
		return false
	}
	c.m[key]++
	return true
}

func (c *keyedCounter) release(key string) {
	if c.max <= 0 || key == "" {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if n := c.m[key]; n <= 1 {
		delete(c.m, key)
	} else {
		c.m[key] = n - 1
	}
}

// connKey buckets by IP: IPv4 (incl. IPv4-mapped) exact, IPv6 by /64.
func connKey(addr net.Addr) string {
	if addr == nil {
		return ""
	}
	host, _, err := net.SplitHostPort(addr.String())
	if err != nil {
		host = addr.String()
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return host
	}
	if v4 := ip.To4(); v4 != nil {
		return v4.String()
	}
	return ip.Mask(net.CIDRMask(64, 128)).String()
}

// Package sigcache memoizes successful signature verifications so the same
// check is not repeated across ante passes, the message server and ABCI phases.
package sigcache

import (
	"crypto/sha256"
	"encoding/binary"

	lru "github.com/hashicorp/golang-lru/v2"
)

// Key identifies one verification.
type Key [sha256.Size]byte

// Domain separates the kinds of verification that share one cache.
type Domain byte

const (
	// PffCertificate covers a MsgPayForFibre validator signature set.
	PffCertificate Domain = iota + 1
	// PromiseSignature covers a payment promise's secp256k1 signature.
	PromiseSignature
	// TxSignature covers a transaction signature.
	TxSignature
)

// NewKey hashes parts under domain. Every part is length-prefixed, so two
// different splits of the same bytes cannot produce the same key.
func NewKey(domain Domain, parts ...[]byte) Key {
	hasher := sha256.New()
	hasher.Write([]byte{byte(domain)})
	var length [8]byte
	for _, part := range parts {
		binary.BigEndian.PutUint64(length[:], uint64(len(part)))
		hasher.Write(length[:])
		hasher.Write(part)
	}

	var key Key
	copy(key[:], hasher.Sum(nil))
	return key
}

// Cache remembers verifications that succeeded. Only successes are admitted, so
// unverifiable spam cannot evict legitimate entries and a hit can only skip
// work that would have passed.
type Cache struct {
	entries *lru.Cache[Key, struct{}]
}

// New returns an empty cache holding at most capacity entries.
func New(capacity int) *Cache {
	entries, err := lru.New[Key, struct{}](capacity)
	if err != nil {
		panic(err)
	}
	return &Cache{entries: entries}
}

// Has reports whether key was already verified.
func (c *Cache) Has(key Key) bool {
	_, ok := c.entries.Get(key)
	return ok
}

// Add records key as verified.
func (c *Cache) Add(key Key) {
	c.entries.Add(key, struct{}{})
}

// Len returns the number of cached entries.
func (c *Cache) Len() int {
	return c.entries.Len()
}

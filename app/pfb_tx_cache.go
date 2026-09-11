package app

import (
	"crypto/sha256"

	"github.com/celestiaorg/go-square/v4/share"
	lru "github.com/hashicorp/golang-lru/v2"
)

const defaultTxCacheCapacity = 10_000

// TxCache caches blob transactions validated in CheckTx so ProcessProposal
// can skip re-validating them. Its fixed capacity bounds memory use.
type TxCache struct {
	entries *lru.Cache[[sha256.Size]byte, [sha256.Size]byte]
}

// NewTxCache creates a new transaction cache
func NewTxCache() *TxCache {
	entries, err := lru.New[[sha256.Size]byte, [sha256.Size]byte](defaultTxCacheCapacity)
	if err != nil {
		panic(err)
	}
	return &TxCache{entries: entries}
}

// getTxKey generates a deterministic key for a transaction
func (c *TxCache) getTxKey(tx []byte) [sha256.Size]byte {
	return sha256.Sum256(tx)
}

// Exists checks whether the Tx exists in the cache and the blobs match the cached blobs
func (c *TxCache) Exists(tx []byte, blobs []*share.Blob) bool {
	txKey := c.getTxKey(tx)
	cachedBlobsHash, found := c.entries.Get(txKey)
	if !found {
		return false
	}

	blobsHash := c.getBlobsHash(blobs)
	return cachedBlobsHash == blobsHash
}

// Set stores the Tx in the cache
func (c *TxCache) Set(tx []byte, blobs []*share.Blob) {
	txKey := c.getTxKey(tx)
	blobsHash := c.getBlobsHash(blobs)
	c.entries.Add(txKey, blobsHash)
}

// getBlobsHash hashes each blob's fixed-size digest, so the boundaries
// between adjacent blobs are unambiguous.
func (c *TxCache) getBlobsHash(blobs []*share.Blob) [sha256.Size]byte {
	hasher := sha256.New()
	for _, blob := range blobs {
		hasher.Write(blob.Hash())
	}

	var blobsHash [sha256.Size]byte
	copy(blobsHash[:], hasher.Sum(nil))
	return blobsHash
}

// Size returns the current number of entries in the cache
func (c *TxCache) Size() int {
	return c.entries.Len()
}

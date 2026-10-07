package app

import (
	"crypto/sha256"

	"github.com/celestiaorg/go-square/v4/share"
	sdk "github.com/cosmos/cosmos-sdk/types"
	lru "github.com/hashicorp/golang-lru/v2"
)

const defaultTxCacheCapacity = 10_000
const maxCachedDecodedPFFBytes = 8 << 10

// TxCache caches transactions validated in CheckTx so ProcessProposal can
// skip redundant validation. A nil blobs value marks an admitted PFF tx.
// Its fixed capacity bounds memory use.
type TxCache struct {
	entries *lru.Cache[[sha256.Size]byte, txCacheEntry]
}

type txCacheEntry struct {
	blobsHash [sha256.Size]byte
	pffTx     sdk.Tx
	isPFF     bool
}

// NewTxCache creates a new transaction cache
func NewTxCache() *TxCache {
	entries, err := lru.New[[sha256.Size]byte, txCacheEntry](defaultTxCacheCapacity)
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
	entry, found := c.entries.Get(txKey)
	if !found {
		return false
	}

	blobsHash := c.getBlobsHash(blobs)
	return entry.blobsHash == blobsHash
}

// Set stores the Tx in the cache
func (c *TxCache) Set(tx []byte, blobs []*share.Blob) {
	txKey := c.getTxKey(tx)
	blobsHash := c.getBlobsHash(blobs)
	c.entries.Add(txKey, txCacheEntry{blobsHash: blobsHash})
}

// SetPFF stores a PFF transaction that passed full CheckTx. The decoded tx is
// reused only for the exact raw transaction hash on this node. Large PFFs keep
// only their admission marker, so decoded objects have a bounded total size.
func (c *TxCache) SetPFF(tx []byte, decoded sdk.Tx) {
	if len(tx) > maxCachedDecodedPFFBytes {
		decoded = nil
	}
	c.entries.Add(c.getTxKey(tx), txCacheEntry{blobsHash: c.getBlobsHash(nil), pffTx: decoded, isPFF: true})
}

// PFFTx returns a previously admitted PFF for the exact raw bytes. The decoded
// transaction is nil when it exceeded the decoded-cache size limit.
func (c *TxCache) PFFTx(tx []byte) (sdk.Tx, bool) {
	entry, found := c.entries.Get(c.getTxKey(tx))
	return entry.pffTx, found && entry.isPFF
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

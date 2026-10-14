package app

import (
	"bytes"
	"sync"

	blobtx "github.com/celestiaorg/go-square/v4/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// proposalKey identifies the inputs a proposal's artifacts were derived from,
// so an entry is never reused across a chain, upgrade, height or square size.
type proposalKey struct {
	chainID       string
	appVersion    uint64
	height        int64
	maxSquareSize int
}

// newProposalKey builds the key for the proposal being built or validated at
// height. The app version comes from the consensus params already on ctx.
func newProposalKey(ctx sdk.Context, height int64, maxSquareSize int) proposalKey {
	params := ctx.ConsensusParams()
	return proposalKey{
		chainID:       ctx.ChainID(),
		appVersion:    params.Version.GetApp(),
		height:        height,
		maxSquareSize: maxSquareSize,
	}
}

// proposalArtifacts is the work PrepareProposal did that the proposer's own
// ProcessProposal would otherwise repeat.
type proposalArtifacts struct {
	key proposalKey
	// txs is a private snapshot of the tx list the artifacts came from. Reuse
	// requires a byte for byte match, so an entry never describes other txs.
	txs [][]byte
	// blobTxs holds the blob tx decoded from txs[i], nil where there is none.
	blobTxs []*blobtx.BlobTx
	// squareSize and dataRoot are what the square, extended square and data
	// availability header built from txs imply.
	squareSize uint64
	dataRoot   []byte
}

// proposalCache holds the artifacts of the one proposal this node built most
// recently. It is a local optimization: a miss recomputes everything and
// reaches the same result. One entry, so nothing outlives its height.
type proposalCache struct {
	mu    sync.Mutex
	entry *proposalArtifacts
}

func newProposalCache() *proposalCache {
	return &proposalCache{}
}

// Store replaces the cached proposal, snapshotting the tx bytes it is keyed on.
func (c *proposalCache) Store(artifacts *proposalArtifacts) {
	artifacts.txs = cloneTxs(artifacts.txs)

	c.mu.Lock()
	defer c.mu.Unlock()
	c.entry = artifacts
}

// cloneTxs copies a tx list so later writes to the caller's buffers cannot
// change what the cache compares against.
func cloneTxs(txs [][]byte) [][]byte {
	cloned := make([][]byte, len(txs))
	for i, tx := range txs {
		cloned[i] = bytes.Clone(tx)
	}
	return cloned
}

// Take empties the cache and returns the entry only if it was built for key
// from exactly these tx bytes. Emptying on every call bounds it to one height.
func (c *proposalCache) Take(key proposalKey, txs [][]byte) *proposalArtifacts {
	c.mu.Lock()
	entry := c.entry
	c.entry = nil
	c.mu.Unlock()

	if entry == nil || entry.key != key {
		return nil
	}
	if len(entry.blobTxs) != len(entry.txs) || !txsEqual(entry.txs, txs) {
		return nil
	}
	return entry
}

// txsEqual reports whether two tx lists are byte for byte identical.
func txsEqual(a, b [][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

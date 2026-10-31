package app

import (
	"sync/atomic"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// txHandoff lets CheckTx pass BaseApp a tx it already decoded. Decoding is a
// pure function of the bytes, so BaseApp gets exactly what it would decode,
// plus the signers and messages a previous pass already derived.
type txHandoff struct {
	lent atomic.Pointer[lentTx]
}

type lentTx struct {
	raw []byte
	tx  sdk.Tx
}

// lend makes the decoder return tx for this exact slice until release runs.
func (h *txHandoff) lend(raw []byte, tx sdk.Tx) (release func()) {
	if len(raw) == 0 {
		return func() {}
	}
	h.lent.Store(&lentTx{raw: raw, tx: tx})
	return func() { h.lent.Store(nil) }
}

// decoder wraps base so the lent slice, compared by identity, decodes to the
// lent tx. Every other call, on any goroutine, decodes as before.
func (h *txHandoff) decoder(base sdk.TxDecoder) sdk.TxDecoder {
	return func(raw []byte) (sdk.Tx, error) {
		if l := h.lent.Load(); l != nil && len(raw) == len(l.raw) && len(raw) > 0 && &raw[0] == &l.raw[0] {
			return l.tx, nil
		}
		return base(raw)
	}
}

// recheckTxCache keeps the decode of each admitted PayForFibre tx, keyed by its
// raw bytes, for the rechecks of those bytes. Callers hold checkStateMu.
type recheckTxCache struct {
	entries    map[string]*recheckTxEntry
	maxEntries int
	// commits counts Commits. Rechecks visit every pending tx after each one,
	// so an entry unused since the previous Commit has left the mempool.
	commits uint64
}

type recheckTxEntry struct {
	tx   sdk.Tx
	used uint64
}

func newRecheckTxCache(maxEntries int) *recheckTxCache {
	return &recheckTxCache{entries: make(map[string]*recheckTxEntry), maxEntries: maxEntries}
}

// add records tx, the decode of raw, once raw passed CheckTx.
func (c *recheckTxCache) add(raw []byte, tx sdk.Tx) {
	if e, ok := c.entries[string(raw)]; ok {
		e.used = c.commits
		return
	}
	if len(c.entries) < c.maxEntries {
		c.entries[string(raw)] = &recheckTxEntry{tx: tx, used: c.commits}
	}
}

func (c *recheckTxCache) get(raw []byte) (sdk.Tx, bool) {
	e, ok := c.entries[string(raw)]
	if !ok {
		return nil, false
	}
	e.used = c.commits
	return e.tx, true
}

// drop removes raw's entry; a failed recheck evicts the tx from the mempool.
func (c *recheckTxCache) drop(raw []byte) {
	delete(c.entries, string(raw))
}

// sweep runs at Commit and drops the entries no CheckTx used since the last one.
func (c *recheckTxCache) sweep() {
	for k, e := range c.entries {
		if e.used < c.commits {
			delete(c.entries, k)
		}
	}
	c.commits++
}

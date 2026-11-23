package app

import (
	"bytes"
	"io"

	storetypes "cosmossdk.io/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// kvOp is one store access of a traced ante pass. A nil value is an absent key
// (read) or a delete (write).
type kvOp struct {
	store      storetypes.StoreKey
	key, value []byte
}

// balanceOps are the accesses to one balance the fee moves: exactly one Get then one Set.
type balanceOps struct {
	key           []byte
	before, after []byte
	gets, sets    int
}

// anteTrace records the store accesses of one full ante pass through its
// multistore, which all ante state access goes through: every value it read that
// it had not written, every write, and the two balances the fee moves. failed is
// set on any access the replay cannot reproduce.
type anteTrace struct {
	bank          storetypes.StoreKey
	payer, fc     balanceOps
	reads, writes []kvOp
	seen          map[string]bool
	// paused is set while the promise check runs: replay re-runs that check.
	paused bool
	failed bool
}

func newAnteTrace(bank storetypes.StoreKey, payerKey, fcKey []byte) *anteTrace {
	return &anteTrace{bank: bank, payer: balanceOps{key: payerKey}, fc: balanceOps{key: fcKey}, seen: map[string]bool{}}
}

func (tr *anteTrace) balance(store storetypes.StoreKey, key []byte) *balanceOps {
	if store != tr.bank {
		return nil
	}
	switch {
	case bytes.Equal(key, tr.payer.key):
		return &tr.payer
	case bytes.Equal(key, tr.fc.key):
		return &tr.fc
	}
	return nil
}

func (tr *anteTrace) onRead(store storetypes.StoreKey, key, value []byte, has bool) {
	if tr.paused {
		return
	}
	if b := tr.balance(store, key); b != nil {
		b.gets++
		tr.failed = tr.failed || has || b.gets > 1 || b.sets > 0
		b.before = value
		return
	}
	// A key read before returns the same value; one written by this pass is derived.
	if id := store.Name() + "\x00" + string(key); !tr.seen[id] {
		tr.seen[id] = true
		tr.reads = append(tr.reads, kvOp{store: store, key: key, value: value})
	}
}

func (tr *anteTrace) onWrite(store storetypes.StoreKey, key, value []byte) {
	if b := tr.balance(store, key); b != nil {
		b.sets++
		tr.failed = tr.failed || value == nil || b.gets != 1 || b.sets > 1
		b.after = value
		return
	}
	tr.failed = tr.failed || tr.paused
	tr.seen[store.Name()+"\x00"+string(key)] = true
	tr.writes = append(tr.writes, kvOp{store: store, key: key, value: value})
}

// traceMS routes every KV access of the ante's multistore branch through tr.
type traceMS struct {
	cms
	tr *anteTrace
}

// cms names the embedded store so traceMS can override CacheMultiStore().
type cms = storetypes.CacheMultiStore

func (m traceMS) GetKVStore(k storetypes.StoreKey) storetypes.KVStore {
	return traceKV{KVStore: m.cms.GetKVStore(k), store: k, tr: m.tr}
}

func (m traceMS) GetStore(k storetypes.StoreKey) storetypes.Store {
	s := m.cms.GetStore(k)
	if kv, ok := s.(storetypes.KVStore); ok {
		return traceKV{KVStore: kv, store: k, tr: m.tr}
	}
	m.tr.failed = true
	return s
}

// Nested branches would hand out untraced stores.
func (m traceMS) CacheMultiStore() storetypes.CacheMultiStore {
	m.tr.failed = true
	return m.cms.CacheMultiStore()
}

func (m traceMS) CacheMultiStoreWithVersion(v int64) (storetypes.CacheMultiStore, error) {
	m.tr.failed = true
	return m.cms.CacheMultiStoreWithVersion(v)
}

func (m traceMS) CacheWrap() storetypes.CacheWrap {
	m.tr.failed = true
	return m.cms.CacheWrap()
}

func (m traceMS) CacheWrapWithTrace(w io.Writer, tc storetypes.TraceContext) storetypes.CacheWrap {
	m.tr.failed = true
	return m.cms.CacheWrapWithTrace(w, tc)
}

type traceKV struct {
	storetypes.KVStore
	store storetypes.StoreKey
	tr    *anteTrace
}

func (s traceKV) Get(key []byte) []byte {
	v := s.KVStore.Get(key)
	s.tr.onRead(s.store, key, v, false)
	return v
}

// Has records the value, as cachekv's Has reads it anyway.
func (s traceKV) Has(key []byte) bool {
	v := s.KVStore.Get(key)
	s.tr.onRead(s.store, key, v, true)
	return v != nil
}

func (s traceKV) Set(key, value []byte) {
	s.tr.onWrite(s.store, key, value)
	s.KVStore.Set(key, value)
}

func (s traceKV) Delete(key []byte) {
	s.tr.onWrite(s.store, key, nil)
	s.KVStore.Delete(key)
}

// Iteration reads a range, not keys the replay can re-read.
func (s traceKV) Iterator(start, end []byte) storetypes.Iterator {
	s.tr.failed = true
	return s.KVStore.Iterator(start, end)
}

func (s traceKV) ReverseIterator(start, end []byte) storetypes.Iterator {
	s.tr.failed = true
	return s.KVStore.ReverseIterator(start, end)
}

func (s traceKV) CacheWrap() storetypes.CacheWrap {
	s.tr.failed = true
	return s.KVStore.CacheWrap()
}

func (s traceKV) CacheWrapWithTrace(w io.Writer, tc storetypes.TraceContext) storetypes.CacheWrap {
	s.tr.failed = true
	return s.KVStore.CacheWrapWithTrace(w, tc)
}

// wrapAnte traces the ante pass CheckTx asked for (fastRecheck.trace). Only
// CheckTx, under App.checkStateMu, reads trace: other passes may run concurrently.
func (app *App) wrapAnte(inner sdk.AnteHandler) sdk.AnteHandler {
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		if !simulate && ctx.IsCheckTx() && app.fastRecheck.trace != nil {
			tr := app.fastRecheck.trace
			if ms, ok := ctx.MultiStore().(storetypes.CacheMultiStore); ok {
				ctx = ctx.WithMultiStore(traceMS{cms: ms, tr: tr})
				// BaseApp loaded the consensus params outside this branch: read them traced.
				app.GetConsensusParams(ctx)
			} else {
				tr.failed = true
			}
		}
		return inner(ctx, tx, simulate)
	}
}

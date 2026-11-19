package app

import (
	"bytes"
	"fmt"
	"io"
	"slices"

	storetypes "cosmossdk.io/store/types"
	"cosmossdk.io/x/feegrant"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	authzkeeper "github.com/cosmos/cosmos-sdk/x/authz/keeper"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

// anteTrace records the state a full ante pass of one tx reads and writes, so
// the fast path's dependency set and write model are derived from what the
// ante actually did rather than from a hand-written list.
type anteTrace struct {
	signer sdk.AccAddress
	// accNumKey is the signer's key in the account-number index.
	accNumKey []byte
	fcAddr    sdk.AccAddress
	// stores are the mounted KV stores; the commit changeset cannot track
	// reads elsewhere (transient, memory stores).
	stores map[string]*storetypes.KVStoreKey
	// deps are the reads the fast path does not re-read itself, as store "\x00" key.
	deps []string
	// reject says why the tx cannot take the fast path ("" if it can).
	reject string
}

func (tr *anteTrace) fail(reason string) {
	if tr.reject == "" {
		tr.reject = reason
	}
}

// isSignerAccount reports whether key in the auth store is the signer's account.
func (tr *anteTrace) isSignerAccount(key []byte) bool {
	return len(key) == 1+len(tr.signer) && key[0] == authtypes.AddressStoreKeyPrefix[0] && bytes.Equal(key[1:], tr.signer)
}

// balanceOwner parses the address of a bank Balances key (prefix, length-prefixed address, denom).
func balanceOwner(key []byte) []byte {
	if len(key) < 2 || key[0] != banktypes.BalancesPrefix[0] {
		return nil
	}
	n := int(key[1])
	if len(key) < 2+n {
		return nil
	}
	return key[2 : 2+n]
}

func (tr *anteTrace) ownBalance(key []byte) bool {
	a := balanceOwner(key)
	return a != nil && (bytes.Equal(a, tr.signer) || bytes.Equal(a, tr.fcAddr))
}

// reread reports whether the fast path re-reads this key on every recheck,
// so a block write to it does not invalidate the record.
func (tr *anteTrace) reread(store string, key []byte) bool {
	switch store {
	case authtypes.StoreKey:
		// The account-number index entry maps the signer's immutable account
		// number to its address; SetAccount re-reads it in the fast path.
		return tr.isSignerAccount(key) || bytes.Equal(key, tr.accNumKey)
	case banktypes.StoreKey:
		return tr.ownBalance(key)
	case fibretypes.StoreKey:
		// The promise check is re-run on an infinite gas meter; params are
		// also read by the rest of the ante, so they stay dependencies.
		return !isFibreParamsKey(key)
	}
	return false
}

func (tr *anteTrace) onRead(store string, key []byte) {
	if tr.stores[store] == nil {
		tr.fail("read:" + store)
		return
	}
	if tr.reread(store, key) {
		return
	}
	// Pool txs' ante writes also change these keys in checkState, so they could
	// revert at Commit without a block write: refuse them as dependencies.
	switch store {
	case authtypes.StoreKey:
		if len(key) > 0 && key[0] == authtypes.AddressStoreKeyPrefix[0] && !bytes.Equal(key[1:], tr.fcAddr) {
			tr.fail("dep:account")
			return
		}
	case banktypes.StoreKey:
		if balanceOwner(key) != nil || (len(key) > 0 && key[0] == banktypes.DenomAddressPrefix[0]) {
			tr.fail("dep:balance")
			return
		}
	case feegrant.StoreKey, authzkeeper.StoreKey:
		tr.fail("dep:" + store)
		return
	}
	tr.deps = append(tr.deps, store+"\x00"+string(key))
}

func newAnteTrace(signer, fcAddr sdk.AccAddress, accNumKey []byte, stores map[string]*storetypes.KVStoreKey) *anteTrace {
	return &anteTrace{signer: signer, fcAddr: fcAddr, accNumKey: accNumKey, stores: stores}
}

// onWrite accepts exactly the writes the fast path replays: the signer's
// account (and its account-number index) and the utia balances (and denom
// index entries) of the signer and the fee collector.
func (tr *anteTrace) onWrite(store string, key []byte) {
	switch store {
	case authtypes.StoreKey:
		if tr.isSignerAccount(key) {
			return
		}
		if bytes.Equal(key, tr.accNumKey) {
			return
		}
	case banktypes.StoreKey:
		if tr.ownBalance(key) {
			return
		}
		if len(key) > 0 && key[0] == banktypes.DenomAddressPrefix[0] &&
			(bytes.HasSuffix(key, tr.signer) || bytes.HasSuffix(key, tr.fcAddr)) {
			return
		}
	}
	p := key
	if len(p) > 1 {
		p = p[:1]
	}
	tr.fail(fmt.Sprintf("write:%s/%x", store, p))
}

// traceMS wraps the ante's multistore branch and routes every KV access
// through tr.
type traceMS struct {
	cmsInner
	tr *anteTrace
}

// cmsInner names the embedded store so traceMS can override CacheMultiStore().
type cmsInner = storetypes.CacheMultiStore

func (m traceMS) GetKVStore(k storetypes.StoreKey) storetypes.KVStore {
	return traceKV{KVStore: m.cmsInner.GetKVStore(k), name: k.Name(), tr: m.tr}
}

func (m traceMS) GetStore(k storetypes.StoreKey) storetypes.Store {
	s := m.cmsInner.GetStore(k)
	if kv, ok := s.(storetypes.KVStore); ok {
		return traceKV{KVStore: kv, name: k.Name(), tr: m.tr}
	}
	m.tr.fail("store:" + k.Name())
	return s
}

// CacheMultiStore: reads through a nested branch would bypass the trace.
func (m traceMS) CacheMultiStore() storetypes.CacheMultiStore {
	m.tr.fail("branch")
	return m.cmsInner.CacheMultiStore()
}

// CacheWrap and CacheWrapWithTrace would also hand out untraced branches.
func (m traceMS) CacheWrap() storetypes.CacheWrap {
	m.tr.fail("branch")
	return m.cmsInner.CacheWrap()
}

func (m traceMS) CacheWrapWithTrace(w io.Writer, tc storetypes.TraceContext) storetypes.CacheWrap {
	m.tr.fail("branch")
	return m.cmsInner.CacheWrapWithTrace(w, tc)
}

func (m traceMS) CacheMultiStoreWithVersion(v int64) (storetypes.CacheMultiStore, error) {
	m.tr.fail("branch")
	return m.cmsInner.CacheMultiStoreWithVersion(v)
}

type traceKV struct {
	storetypes.KVStore
	name string
	tr   *anteTrace
}

func (s traceKV) Get(key []byte) []byte {
	s.tr.onRead(s.name, key)
	return s.KVStore.Get(key)
}

func (s traceKV) Has(key []byte) bool {
	s.tr.onRead(s.name, key)
	return s.KVStore.Has(key)
}

func (s traceKV) Set(key, value []byte) {
	s.tr.onWrite(s.name, key)
	s.KVStore.Set(key, value)
}

func (s traceKV) Delete(key []byte) {
	s.tr.onWrite(s.name, key)
	s.KVStore.Delete(key)
}

func (s traceKV) Iterator(start, end []byte) storetypes.Iterator {
	s.tr.fail("iter:" + s.name)
	return s.KVStore.Iterator(start, end)
}

func (s traceKV) ReverseIterator(start, end []byte) storetypes.Iterator {
	s.tr.fail("iter:" + s.name)
	return s.KVStore.ReverseIterator(start, end)
}

func (s traceKV) CacheWrap() storetypes.CacheWrap {
	s.tr.fail("cachewrap:" + s.name)
	return s.KVStore.CacheWrap()
}

func (s traceKV) CacheWrapWithTrace(w io.Writer, tc storetypes.TraceContext) storetypes.CacheWrap {
	s.tr.fail("cachewrap:" + s.name)
	return s.KVStore.CacheWrapWithTrace(w, tc)
}

// wrapAnte traces the ante pass CheckTx asked for (fastRecheck.trace). It is
// installed only when fast recheck is on.
func (app *App) wrapAnte(inner sdk.AnteHandler) sdk.AnteHandler {
	return func(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
		if mode := ctx.ExecMode(); !simulate && (mode == sdk.ExecModeCheck || mode == sdk.ExecModeReCheck) {
			if tr := app.fastRecheck.trace; tr != nil {
				if ms, ok := ctx.MultiStore().(storetypes.CacheMultiStore); ok {
					ctx = ctx.WithMultiStore(traceMS{cmsInner: ms, tr: tr})
				} else {
					tr.fail("ms")
				}
			}
		}
		return inner(ctx, tx, simulate)
	}
}

// depSet is an interned dependency set shared by records with the same reads.
type depSet struct {
	keys      []string
	checkedAt uint64
	lastWrite uint64
	lastKey   string
}

// lastBlockWrite returns the latest commit counter at which a block wrote one
// of ds's keys, computed once per commit.
func (f *fastRecheck) lastBlockWrite(ds *depSet) uint64 {
	if ds.checkedAt != f.commits {
		var m uint64
		for _, k := range ds.keys {
			if w := f.watched[k]; w > m {
				m = w
				ds.lastKey = k
			}
		}
		ds.lastWrite = m
		ds.checkedAt = f.commits
	}
	return ds.lastWrite
}

// internDeps returns the shared depSet of the dependency keys (sorted and deduplicated in place).
func (f *fastRecheck) internDeps(keys []string) *depSet {
	slices.Sort(keys)
	keys = slices.Compact(keys)
	var b bytes.Buffer
	for _, k := range keys {
		b.WriteString(k)
		b.WriteByte(0xff)
	}
	id := b.String()
	if ds, ok := f.depsets[id]; ok {
		return ds
	}
	ds := &depSet{keys: keys}
	for _, k := range keys {
		if _, ok := f.watched[k]; !ok {
			f.watched[k] = 0
		}
	}
	f.depsets[id] = ds
	return ds
}

// gcDeps drops depsets and watched keys no live record uses.
func (f *fastRecheck) gcDeps() {
	live := make(map[*depSet]bool)
	for _, r := range f.records {
		live[r.deps] = true
	}
	watched := make(map[string]uint64)
	for id, ds := range f.depsets {
		if !live[ds] {
			delete(f.depsets, id)
			continue
		}
		for _, k := range ds.keys {
			watched[k] = f.watched[k]
		}
	}
	f.watched = watched
}

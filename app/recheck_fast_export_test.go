package app

import (
	"bytes"
	"fmt"
	"maps"
	"slices"
	"time"

	"cosmossdk.io/core/header"
	storetypes "cosmossdk.io/store/types"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// FastRecheckStats returns the fast path's hits and misses since the last Commit.
func (app *App) FastRecheckStats() (hits, misses uint64) {
	return app.fastRecheck.hits, app.fastRecheck.misses
}

// FastRecheckRecords is the number of fast recheck records.
func (app *App) FastRecheckRecords() int { return len(app.fastRecheck.records) }

// ResetFastRecheck drops every record, as new consensus params would.
func (app *App) ResetFastRecheck() {
	clear(app.fastRecheck.records)
	app.fastRecheck.bytes = 0
}

// KVStoreKeyNames lists the mounted KV store names.
func (app *App) KVStoreKeyNames() []string {
	return slices.Sorted(maps.Keys(app.keys))
}

// FastRecheckGuardedReads takes every record that replays on a branch of check
// state, changes each value it read in turn, and counts the changes that did not
// stop the replay.
func (app *App) FastRecheckGuardedReads() (reads, unguarded, replayable int, stores map[string]bool) {
	f := app.fastRecheck
	ctx, _ := app.CheckState()
	branch := func() sdk.Context { return ctx.WithMultiStore(ctx.MultiStore().CacheMultiStore()) }
	stores = map[string]bool{}
	for _, rec := range f.records {
		if _, ok := f.replay(branch(), rec); !ok {
			continue
		}
		replayable++
		for _, r := range rec.reads {
			reads++
			stores[r.store.Name()] = true
			b := branch()
			kv := b.MultiStore().GetKVStore(r.store)
			switch {
			case r.value == nil:
				kv.Set(r.key, []byte{}) // present but empty, like a KeySet entry
			case len(r.value) == 0:
				kv.Delete(r.key)
			default:
				kv.Set(r.key, append(bytes.Clone(r.value), 0))
			}
			if _, ok := f.replay(b, rec); ok {
				unguarded++
			}
		}
	}
	return reads, unguarded, replayable, stores
}

// FastRecheckGuardedContext replays tx's record on check state with moved
// block times and heights, and compares each replay with the full recheck ante:
// verdict, gas and every store. It returns the replays and their disagreements.
func (app *App) FastRecheckGuardedContext(tx []byte) (replayed int, unsound []string) {
	key := tx
	if btx, isBlob, err := blobtx.UnmarshalBlobTxNoCopy(tx); isBlob && err == nil {
		key = btx.Tx
	}
	rec := app.fastRecheck.records[string(key)]
	if rec == nil {
		return 0, nil
	}
	sdkTx, err := app.encodingConfig.TxConfig.TxDecoder()(key)
	if err != nil {
		return 0, []string{err.Error()}
	}
	base, _ := app.CheckState()
	base = base.WithConsensusParams(app.GetConsensusParams(base))
	for _, shift := range []struct {
		time   time.Duration
		height int64
	}{{0, 0}, {time.Second, 1}, {time.Hour, 100}, {10 * 365 * 24 * time.Hour, 1_000_000}} {
		ctx := base.WithBlockTime(base.BlockTime().Add(shift.time)).WithBlockHeight(base.BlockHeight() + shift.height)
		ctx = ctx.WithHeaderInfo(header.Info{Height: ctx.BlockHeight(), Time: ctx.BlockTime(), ChainID: ctx.ChainID()})
		fast := ctx.WithMultiStore(ctx.MultiStore().CacheMultiStore())
		res, ok := app.fastRecheck.replay(fast, rec)
		if !ok {
			continue
		}
		replayed++
		fail := func(format string, args ...any) {
			unsound = append(unsound, fmt.Sprintf("shifted by %v/%d: ", shift.time, shift.height)+fmt.Sprintf(format, args...))
		}
		full := ctx.WithMultiStore(ctx.MultiStore().CacheMultiStore()).WithIsReCheckTx(true).
			WithTxBytes(key).WithGasMeter(storetypes.NewInfiniteGasMeter())
		after, err := app.AnteHandler()(full, sdkTx, false)
		if err != nil {
			fail("the full recheck rejects: %v", err)
			continue
		}
		if gas := int64(after.GasMeter().GasConsumed()); gas != res.GasUsed {
			fail("gas used %d, full recheck %d", res.GasUsed, gas)
		}
		for _, name := range app.KVStoreKeyNames() {
			k := app.keys[name]
			if diff := firstKVDiff(fast.MultiStore().GetKVStore(k), full.MultiStore().GetKVStore(k)); diff != "" {
				fail("store %s: %s", name, diff)
			}
		}
	}
	return replayed, unsound
}

// firstKVDiff describes the first key at which a and b differ, or returns "".
func firstKVDiff(a, b storetypes.KVStore) string {
	ia, ib := a.Iterator(nil, nil), b.Iterator(nil, nil)
	defer ia.Close()
	defer ib.Close()
	for ; ia.Valid() && ib.Valid(); ia.Next() {
		if !bytes.Equal(ia.Key(), ib.Key()) || !bytes.Equal(ia.Value(), ib.Value()) {
			return fmt.Sprintf("replay %x=%x, full %x=%x", ia.Key(), ia.Value(), ib.Key(), ib.Value())
		}
		ib.Next()
	}
	if ia.Valid() || ib.Valid() {
		return "one side has extra keys"
	}
	return ""
}

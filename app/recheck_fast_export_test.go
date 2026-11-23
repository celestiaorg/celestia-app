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
	abci "github.com/cometbft/cometbft/abci/types"
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
	rec, key, sdkTx, err := app.fastRecheckRecord(tx)
	if err != nil {
		return 0, []string{err.Error()}
	}
	if rec == nil {
		return 0, nil
	}
	base, _ := app.CheckState()
	for _, shift := range []struct {
		time   time.Duration
		height int64
	}{{0, 0}, {time.Second, 1}, {time.Hour, 100}, {10 * 365 * 24 * time.Hour, 1_000_000}} {
		ctx := base.WithBlockTime(base.BlockTime().Add(shift.time)).WithBlockHeight(base.BlockHeight() + shift.height)
		ctx = ctx.WithHeaderInfo(header.Info{Height: ctx.BlockHeight(), Time: ctx.BlockTime(), ChainID: ctx.ChainID()})
		ok, diffs := app.compareReplay(ctx, rec, key, sdkTx)
		if !ok {
			continue
		}
		replayed++
		for _, d := range diffs {
			unsound = append(unsound, fmt.Sprintf("shifted by %v/%d: %s", shift.time, shift.height, d))
		}
	}
	return replayed, unsound
}

// FastRecheckUnrecordedState checks that tx's record read every stored value
// its full recheck ante depends on. For each key of check state the record did
// not read, in every KV store, it deletes the key on a branch, then on another
// sets it to its value with 0x00 appended, and compares each change the replay
// still accepts with the full recheck ante, as FastRecheckGuardedContext does.
// stride > 1 changes only every stride-th such key of each store. It returns
// the changes, the replays and their disagreements.
//
// Only existing keys are changed: a read of an absent key that the trace
// missed goes unnoticed.
func (app *App) FastRecheckUnrecordedState(tx []byte, stride int) (changes, replayed int, unsound []string) {
	rec, key, sdkTx, err := app.fastRecheckRecord(tx)
	if err != nil {
		return 0, 0, []string{err.Error()}
	}
	if rec == nil {
		return 0, 0, nil
	}
	stride = max(stride, 1)
	base, _ := app.CheckState()
	branch := func() sdk.Context { return base.WithMultiStore(base.MultiStore().CacheMultiStore()) }
	if _, ok := app.fastRecheck.replay(branch(), rec); !ok {
		return 0, 0, nil
	}
	read := map[string]bool{}
	for _, r := range rec.reads {
		read[r.store.Name()+"\x00"+string(r.key)] = true
	}
	for _, name := range app.KVStoreKeyNames() {
		sk := app.keys[name]
		var keys [][]byte
		it := base.MultiStore().GetKVStore(sk).Iterator(nil, nil)
		for i := 0; it.Valid(); it.Next() {
			if read[name+"\x00"+string(it.Key())] {
				continue
			}
			if i%stride == 0 {
				keys = append(keys, bytes.Clone(it.Key()))
			}
			i++
		}
		it.Close()
		for _, k := range keys {
			for _, change := range []string{"deleted", "extended"} {
				ctx := branch()
				kv := ctx.MultiStore().GetKVStore(sk)
				if change == "deleted" {
					kv.Delete(k)
				} else {
					kv.Set(k, append(bytes.Clone(kv.Get(k)), 0))
				}
				changes++
				ok, diffs := app.compareReplay(ctx, rec, key, sdkTx)
				if !ok {
					continue
				}
				replayed++
				for _, d := range diffs {
					unsound = append(unsound, fmt.Sprintf("store %s key %x %s: %s", name, k, change, d))
				}
			}
		}
	}
	return changes, replayed, unsound
}

// fastRecheckRecord returns tx's record (nil if none), the bytes of the sdk tx
// it was recorded under, and that tx decoded.
func (app *App) fastRecheckRecord(tx []byte) (rec *fastRecord, key []byte, sdkTx sdk.Tx, err error) {
	key = tx
	if btx, isBlob, err := blobtx.UnmarshalBlobTxNoCopy(tx); isBlob && err == nil {
		key = btx.Tx
	}
	rec = app.fastRecheck.records[string(key)]
	if rec == nil {
		return nil, key, nil, nil
	}
	sdkTx, err = app.encodingConfig.TxConfig.TxDecoder()(key)
	return rec, key, sdkTx, err
}

// compareReplay replays rec on a branch of ctx and, if it replays, runs the
// full recheck ante of sdkTx (encoded as key) on a twin branch. It reports
// whether rec replayed and each disagreement: verdict, gas used or any store.
func (app *App) compareReplay(ctx sdk.Context, rec *fastRecord, key []byte, sdkTx sdk.Tx) (replayed bool, unsound []string) {
	ctx = ctx.WithConsensusParams(app.GetConsensusParams(ctx))
	fast := ctx.WithMultiStore(ctx.MultiStore().CacheMultiStore())
	res, ok := app.replayRecovered(fast, rec)
	if !ok {
		return false, nil
	}
	full := ctx.WithMultiStore(ctx.MultiStore().CacheMultiStore()).WithIsReCheckTx(true).
		WithTxBytes(key).WithGasMeter(storetypes.NewInfiniteGasMeter())
	after, err := app.fullRecheckAnte(full, sdkTx)
	if err != nil {
		return true, []string{fmt.Sprintf("the full recheck rejects: %v", err)}
	}
	if gas := int64(after.GasMeter().GasConsumed()); gas != res.GasUsed {
		unsound = append(unsound, fmt.Sprintf("gas used %d, full recheck %d", res.GasUsed, gas))
	}
	for _, name := range app.KVStoreKeyNames() {
		k := app.keys[name]
		if diff := firstKVDiff(fast.MultiStore().GetKVStore(k), full.MultiStore().GetKVStore(k)); diff != "" {
			unsound = append(unsound, fmt.Sprintf("store %s: %s", name, diff))
		}
	}
	return true, unsound
}

// replayRecovered replays rec on ctx. A changed value can be malformed for the
// promise check the replay re-runs, which then panics: that is a refusal.
func (app *App) replayRecovered(ctx sdk.Context, rec *fastRecord) (res *abci.ResponseCheckTx, ok bool) {
	defer func() {
		if recover() != nil {
			res, ok = nil, false
		}
	}()
	return app.fastRecheck.replay(ctx, rec)
}

// fullRecheckAnte runs the full recheck ante on ctx, turning a panic into an
// error as BaseApp does.
func (app *App) fullRecheckAnte(ctx sdk.Context, sdkTx sdk.Tx) (after sdk.Context, err error) {
	defer func() {
		if r := recover(); r != nil {
			err = fmt.Errorf("panic: %v", r)
		}
	}()
	return app.AnteHandler()(ctx, sdkTx, false)
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

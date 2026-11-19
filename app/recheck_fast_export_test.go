package app

import (
	"bytes"
	"maps"
	"slices"

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

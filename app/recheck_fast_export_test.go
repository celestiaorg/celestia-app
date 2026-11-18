package app

import (
	"maps"
	"slices"
)

// FastRecheckGasPerByte is the per-byte gas bound the fast path assumes.
const FastRecheckGasPerByte = fastRecheckGasPerByte

// FastRecheckStats returns the fast path's hits, misses and miss reasons since the last Commit.
func (app *App) FastRecheckStats() (hits, misses uint64, reasons map[string]uint64) {
	f := app.fastRecheck
	return f.stats.hits, f.stats.misses, maps.Clone(f.stats.missReasons)
}

// FastRecheckLastDelta is the balance-length difference behind the last hit;
// its GasUsed is exact when this is zero.
func (app *App) FastRecheckLastDelta() uint64 { return app.fastRecheck.lastDelta }

// FastRecheckVerifyStats returns the shadow-verified hits and mismatches so far.
func (app *App) FastRecheckVerifyStats() (verified, mismatches uint64) {
	v := app.fastRecheck.verify
	if v == nil {
		return 0, 0
	}
	return v.totalVerified + v.verified, v.totalMismatches + v.mismatches
}

// FastRecheckTamperGas shifts every record's GasUsed, so shadow verify must report it.
func (app *App) FastRecheckTamperGas(delta int64) {
	for _, r := range app.fastRecheck.records {
		r.gasUsed = uint64(int64(r.gasUsed) + delta)
	}
}

// SetFastRecheckForceBump drops every record every n commits, as a param change would.
func (app *App) SetFastRecheckForceBump(n uint64) { app.fastRecheck.forceBumpEvery = n }

// FastRecheckRecords is the number of fast recheck records.
func (app *App) FastRecheckRecords() int { return len(app.fastRecheck.records) }

// KVStoreKeyNames lists the mounted KV store names.
func (app *App) KVStoreKeyNames() []string {
	return slices.Sorted(maps.Keys(app.keys))
}

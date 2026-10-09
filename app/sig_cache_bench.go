//go:build benchmarks

package app

// PurgeNodeCaches empties every cache this node keeps in memory, so a benchmark
// can measure the path a restarted or lagging node takes: nothing pre-verified,
// nothing pre-decoded, every transaction processed in full.
//
// Benchmarks only. A node never discards work it already paid for.
func (app *App) PurgeNodeCaches() {
	app.sigCache.Purge()
	app.txCache.entries.Purge()
	app.pffTxCache.purge()
	app.proposalCache.purge()
	app.FibreKeeper.PurgeValidatorSetCache()
}

// purge drops every cached PayForFibre tx and the byte accounting with it.
func (c *pffTxCache) purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entries.Purge()
	c.bytes = 0
}

// purge drops the stored proposal artifacts.
func (c *proposalCache) purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entry = nil
}

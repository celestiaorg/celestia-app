//go:build benchmarks

package app

// PurgeNodeCaches empties every in-memory cache, so a benchmark can measure the
// path a restarted node takes. Benchmarks only: a node never discards paid work.
func (app *App) PurgeNodeCaches() {
	app.sigCache.Purge()
	app.txCache.entries.Purge()
	app.pffTxCache.purge()
	app.proposalCache.purge()
	app.FibreKeeper.PurgeValidatorSetCache()
}

// purge drops every cached PayForFibre tx. The LRU does its own locking.
func (c *pffTxCache) purge() {
	c.entries.Purge()
}

// purge drops the stored proposal artifacts.
func (c *proposalCache) purge() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.entry = nil
}

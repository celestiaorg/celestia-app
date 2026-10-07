//go:build benchmarks

package app

// PurgeNodeCaches clears validation caches between cold benchmark iterations.
func (app *App) PurgeNodeCaches() {
	app.pffSigCache.entries.Purge()
	app.txCache.entries.Purge()
}

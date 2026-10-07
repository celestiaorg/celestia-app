//go:build benchmarks

package app

import "github.com/celestiaorg/celestia-app/v10/fibre/validator"

// PurgeNodeCaches clears validation caches between cold benchmark iterations.
func (app *App) PurgeNodeCaches() {
	app.pffSigCache.entries.Purge()
	app.txSigCache.Purge()
	app.txCache.entries.Purge()
	validator.PurgeExpandedValidatorKeys()
}

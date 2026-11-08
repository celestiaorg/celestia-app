//go:build pffcampaign

package app

func (app *App) campaignPurgeRecheck() {
	app.checkStateMu.Lock()
	defer app.checkStateMu.Unlock()
	app.recheckTxs = newRecheckTxCache(app.recheckTxs.maxEntries)
}
func (app *App) campaignRecheckStats(stats map[string]int) {
	stats["decoded_entries"] = len(app.recheckTxs.entries)
	stats["decoded_capacity"] = app.recheckTxs.maxEntries
	stats["decoded_bytes"] = app.recheckTxs.bytes
	stats["decoded_byte_capacity"] = app.recheckTxs.maxBytes
}

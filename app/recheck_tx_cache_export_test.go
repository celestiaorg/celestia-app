package app

// RecheckTxCached reports whether a recheck of tx would reuse its admitted decode.
func (app *App) RecheckTxCached(tx []byte) bool {
	_, ok := app.recheckTxs.entries[string(tx)]
	return ok
}

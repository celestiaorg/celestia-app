package app

// RecheckTxCached reports whether a recheck of tx would reuse its admitted decode.
func (app *App) RecheckTxCached(tx []byte) bool {
	_, ok := app.recheckTxs.entries[string(tx)]
	return ok
}

// SwapRecheckTxs swaps the decodes kept for a and b.
func (app *App) SwapRecheckTxs(a, b []byte) {
	ea, eb := app.recheckTxs.entries[string(a)], app.recheckTxs.entries[string(b)]
	ea.tx, eb.tx = eb.tx, ea.tx
}

// DisableTxReuse makes app decode every tx afresh and keep no decodes, as main.
func (app *App) DisableTxReuse() {
	app.txHandoff = &txHandoff{}
	app.recheckTxs = newRecheckTxCache(0)
}

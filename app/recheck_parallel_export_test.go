package app

// SetBeforeRecheckMerge runs fn before each parallel recheck merges; an error makes it fall back.
func (app *App) SetBeforeRecheckMerge(fn func() error) { app.beforeRecheckMerge = fn }

// RecheckGroups runs txs as groups on workers, as a parallel RecheckTxs would, under the
// app's check state lock.
func (app *App) RecheckGroups(txs [][]byte, groups [][]int, workers int) error {
	app.checkStateMu.Lock()
	defer app.checkStateMu.Unlock()
	_, err := app.recheckGroups(txs, groups, workers)
	return err
}

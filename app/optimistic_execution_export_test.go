package app

// OptimisticExecutionTxs returns txs as optimistic execution would run them
// after the app's last ProcessProposal.
func (app *App) OptimisticExecutionTxs(txs [][]byte) [][]byte {
	return app.oeTxs.strip(txs)
}

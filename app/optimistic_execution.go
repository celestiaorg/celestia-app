package app

import coretypes "github.com/cometbft/cometbft/types"

// oeTxsTransform turns ProcessProposal txs into the txs FinalizeBlock will get.
// ProcessProposalHandler sets appVersion just before BaseApp starts the run.
type oeTxsTransform struct {
	appVersion uint64
}

func (t *oeTxsTransform) strip(txs [][]byte) [][]byte {
	return stripBlobTxs(txs, t.appVersion)
}

// stripBlobTxs mirrors celestia-core's unwrapBlobTxs (state/execution.go), which
// unwraps a block's txs under the BlobTx rules of its app version.
func stripBlobTxs(txs [][]byte, appVersion uint64) [][]byte {
	stripped := make([][]byte, len(txs))
	for i, tx := range txs {
		stripped[i], _ = coretypes.UnwrapBlobTx(tx, appVersion)
	}
	return stripped
}

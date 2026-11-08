package app

import coretypes "github.com/cometbft/cometbft/types"

// stripBlobTxs returns the txs as celestia-core hands them to FinalizeBlock: BlobTx
// envelopes replaced by their inner tx (state/execution.go applyBlock).
func stripBlobTxs(txs [][]byte) [][]byte {
	stripped := make([][]byte, len(txs))
	for i, tx := range txs {
		if blobTx, isBlobTx := coretypes.UnmarshalBlobTx(tx); isBlobTx {
			tx = blobTx.Tx
		}
		stripped[i] = tx
	}
	return stripped
}

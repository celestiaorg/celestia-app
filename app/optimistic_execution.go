package app

import coretypes "github.com/cometbft/cometbft/types"

// stripBlobTxs maps ProcessProposal txs to what core sends FinalizeBlock. One version is enough: ProcessProposal
// rejects any BlobTx the validating decode fails, and OE only runs on accepted blocks.
func stripBlobTxs(txs [][]byte) [][]byte {
	stripped := make([][]byte, len(txs))
	for i, tx := range txs {
		stripped[i], _ = coretypes.UnwrapBlobTx(tx, coretypes.BlobTxValidationAppVersion)
	}
	return stripped
}

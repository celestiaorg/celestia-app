package app

import (
	"github.com/cosmos/cosmos-sdk/client"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// separateTxs decodes raw tendermint txs into normal and blob txs.
func separateTxs(txcfg client.TxConfig, rawTxs [][]byte) ([][]byte, []core.BlobTx) {
	normalTxs := make([][]byte, 0, len(rawTxs))
	blobTxs := make([]core.BlobTx, 0, len(rawTxs))
	for _, rawTx := range rawTxs {
		bTx, isBlob := coretypes.UnmarshalBlobTx(rawTx)
		if isBlob {
			blobTxs = append(blobTxs, bTx)
		} else {
			normalTxs = append(normalTxs, rawTx)
		}
	}
	return normalTxs, blobTxs
}

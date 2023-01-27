package app

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
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

// filterStdTxsWithAnteHandler applies the provided antehandler to each transaction
// and removes transactions that return an error.
func filterStdTxsWithAnteHandler(dec sdk.TxDecoder, ctx sdk.Context, handler sdk.AnteHandler, txs [][]byte) ([][]byte, sdk.Context) {
	valid := func(tx []byte) bool {
		sdkTx, err := dec(tx)
		if err != nil {
			return false
		}
		ctx, err = handler(ctx, sdkTx, false)
		return err == nil
	}

	n := 0
	for _, tx := range txs {
		if valid(tx) {
			txs[n] = tx
			n++
		}
	}

	return txs[:n], ctx
}

// filterStdTxsWithAnteHandler applies the provided antehandler to each transaction
// and removes transactions that return an error.
func filterBlobTxsWithAnteHandler(dec sdk.TxDecoder, ctx sdk.Context, handler sdk.AnteHandler, txs []tmproto.BlobTx) ([]tmproto.BlobTx, sdk.Context) {
	valid := func(tx []byte) bool {
		sdkTx, err := dec(tx)
		if err != nil {
			return false
		}
		ctx, err = handler(ctx, sdkTx, false)
		return err == nil
	}

	n := 0
	for _, tx := range txs {
		if valid(tx.Tx) {
			txs[n] = tx
			n++
		}
	}

	return txs[:n], ctx
}

package app

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// separateTxs decodes raw tendermint txs into normal and blob txs.
func separateTxs(_ client.TxConfig, rawTxs [][]byte) ([][]byte, []core.BlobTx) {
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

// filterTxs applies the antehandler to all proposed transactions and removes transactions that return an error.
func filterTxs(ctx sdk.Context, handler sdk.AnteHandler, txConfig client.TxConfig, txs [][]byte) [][]byte {
	normalTxs, blobTxs := separateTxs(txConfig, txs)
	normalTxs, ctx = filterStdTxs(txConfig.TxDecoder(), ctx, handler, normalTxs)
	blobTxs, _ = filterBlobTxs(txConfig.TxDecoder(), ctx, handler, blobTxs)
	return append(normalTxs, encodeBlobTxs(blobTxs)...)
}

// filterStdTxs applies the provided antehandler to each transaction and removes
// transactions that return an error. Panics are caught by the checkTxValidity
// function used to apply the ante handler.
func filterStdTxs(dec sdk.TxDecoder, ctx sdk.Context, handler sdk.AnteHandler, txs [][]byte) ([][]byte, sdk.Context) {
	n := 0
	var err error
	for _, tx := range txs {
		ctx, err = checkTxValidity(dec, ctx, handler, tx)
		// either the transaction is invalid (ie incorrect nonce) and we
		// simply want to remove this tx, or we're catching a panic from one
		// of the anteHanders which is logged.
		if err != nil {
			continue
		}
		txs[n] = tx
		n++

	}

	return txs[:n], ctx
}

// filterBlobTxs applies the provided antehandler to each transaction
// and removes transactions that return an error. Panics are caught by the checkTxValidity
// function used to apply the ante handler.
func filterBlobTxs(dec sdk.TxDecoder, ctx sdk.Context, handler sdk.AnteHandler, txs []tmproto.BlobTx) ([]tmproto.BlobTx, sdk.Context) {
	n := 0
	var err error
	for _, tx := range txs {
		ctx, err = checkTxValidity(dec, ctx, handler, tx.Tx)
		// either the transaction is invalid (ie incorrect nonce) and we
		// simply want to remove this tx, or we're catching a panic from one
		// of the anteHanders which is logged.
		if err != nil {
			continue
		}
		txs[n] = tx
		n++

	}

	return txs[:n], ctx
}

func checkTxValidity(dec sdk.TxDecoder, ctx sdk.Context, handler sdk.AnteHandler, tx []byte) (sdk.Context, error) {
	sdkTx, err := dec(tx)
	if err != nil {
		return ctx, err
	}

	return handler(ctx, sdkTx, false)
}

func encodeBlobTxs(blobTxs []tmproto.BlobTx) [][]byte {
	txs := make([][]byte, len(blobTxs))
	var err error
	for i, tx := range blobTxs {
		txs[i], err = coretypes.MarshalBlobTx(tx.Tx, tx.Blobs...)
		if err != nil {
			panic(err)
		}
	}
	return txs
}

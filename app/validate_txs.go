package app

import (
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/go-square/v2/tx"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	"github.com/tendermint/tendermint/libs/log"
	coretypes "github.com/tendermint/tendermint/types"
)

// separateTxs decodes raw tendermint txs into normal and blob txs.
func separateTxs(_ client.TxConfig, rawTxs [][]byte) ([][]byte, []*tx.BlobTx) {
	normalTxs := make([][]byte, 0, len(rawTxs))
	blobTxs := make([]*tx.BlobTx, 0, len(rawTxs))
	for _, rawTx := range rawTxs {
		bTx, isBlob, err := tx.UnmarshalBlobTx(rawTx)
		if isBlob {
			if err != nil {
				panic(err)
			}
			blobTxs = append(blobTxs, bTx)
		} else {
			normalTxs = append(normalTxs, rawTx)
		}
	}
	return normalTxs, blobTxs
}

// FilterTxs applies the antehandler to all proposed transactions and removes
// transactions that return an error.
//
// Side-effect: arranges all normal transactions before all blob transactions.
func FilterTxs(logger log.Logger, ctx sdk.Context, handler sdk.AnteHandler, txConfig client.TxConfig, txs [][]byte) [][]byte {
	normalTxs, blobTxs := separateTxs(txConfig, txs)
	normalTxs, ctx = filterStdTxs(logger, txConfig.TxDecoder(), ctx, handler, normalTxs)
	blobTxs, _ = filterBlobTxs(logger, txConfig.TxDecoder(), ctx, handler, blobTxs)
	return append(normalTxs, encodeBlobTxs(blobTxs)...)
}

// filterStdTxs applies the provided antehandler to each transaction and removes
// transactions that return an error. Panics are not caught here;
// invalid transactions are skipped with logging.
func filterStdTxs(logger log.Logger, dec sdk.TxDecoder, ctx sdk.Context, handler sdk.AnteHandler, txs [][]byte) ([][]byte, sdk.Context) {
	n := 0
	nonPFBMessageCount := 0
	for _, tx := range txs {
		sdkTx, err := dec(tx)
                txHash := tmbytes.HexBytes(coretypes.Tx(tx).Hash())
		if err != nil {
			logger.Error("decoding already checked transaction", "tx", txHash, "error", err)
			continue
		}

		// Set the tx size on the context before calling the AnteHandler
		ctx = ctx.WithTxBytes(tx)

		msgs := sdkTx.GetMsgs()
		msgTypes := msgTypes(sdkTx)
		if nonPFBMessageCount+len(msgs) > appconsts.MaxNonPFBMessages {
			logger.Debug("skipping tx because the max non PFB message count was reached", "tx", txHash)
			continue
		}
		nonPFBMessageCount += len(msgs)

		ctx, err = handler(ctx, sdkTx, false)
		// either the transaction is invalid (ie incorrect nonce) and we
		// simply want to remove this tx, or we're catching a panic from one
		// of the anteHandlers which is logged.
		if err != nil {
			logger.Error(
				"filtering already checked transaction",
				"tx", txHash,
				"error", err,
				"msgs", msgTypes,
			)
			telemetry.IncrCounter(1, "prepare_proposal", "invalid_std_txs")
			continue
		}
		txs[n] = tx
		n++

	}

	return txs[:n], ctx
}

// filterBlobTxs applies the provided antehandler to each transaction
// and removes transactions that return an error. Panics are not caught here;
// invalid transactions are skipped with logging.
func filterBlobTxs(logger log.Logger, dec sdk.TxDecoder, ctx sdk.Context, handler sdk.AnteHandler, txs []*tx.BlobTx) ([]*tx.BlobTx, sdk.Context) {
	n := 0
	pfbMessageCount := 0
	for _, tx := range txs {
		txHash := tmbytes.HexBytes(coretypes.Tx(tx.Tx).Hash())
		sdkTx, err := dec(tx.Tx)
		if err != nil {
			logger.Error("decoding already checked blob transaction", "tx", txHash, "error", err)
			continue
		}

		// Set the tx size on the context before calling the AnteHandler
		ctx = ctx.WithTxBytes(tx.Tx)
		msgs := sdkTx.GetMsgs()

		if pfbMessageCount+len(msgs) > appconsts.MaxPFBMessages {
			logger.Debug("skipping tx because the max pfb message count was reached", "tx", txHash)
			continue
		}
		pfbMessageCount += len(msgs)

		ctx, err = handler(ctx, sdkTx, false)
		// either the transaction is invalid (ie incorrect nonce) and we
		// simply want to remove this tx, or we're catching a panic from one
		// of the anteHandlers which is logged.
		if err != nil {
			logger.Error(
				"filtering already checked blob transaction", "tx", txHash, "error", err,
			)
			telemetry.IncrCounter(1, "prepare_proposal", "invalid_blob_txs")
			continue
		}
		txs[n] = tx
		n++

	}

	return txs[:n], ctx
}

func msgTypes(sdkTx sdk.Tx) []string {
	msgs := sdkTx.GetMsgs()
	msgNames := make([]string, len(msgs))
	for i, msg := range msgs {
		msgNames[i] = sdk.MsgTypeURL(msg)
	}
	return msgNames
}

func encodeBlobTxs(blobTxs []*tx.BlobTx) [][]byte {
	txs := make([][]byte, len(blobTxs))
	var err error
	for i, blobTx := range blobTxs {
		txs[i], err = tx.MarshalBlobTx(blobTx.Tx, blobTx.Blobs...)
		if err != nil {
			panic(err)
		}
	}
	return txs
}

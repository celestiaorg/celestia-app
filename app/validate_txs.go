package app

import (
	v3consts "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v3"
	types2 "github.com/celestiaorg/celestia-app/v3/x/blob/types"
	"github.com/celestiaorg/go-square/v2/tx"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/bank/types"
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
// transactions that return an error. Panics are caught by the checkTxValidity
// function used to apply the ante handler.
func filterStdTxs(logger log.Logger, dec sdk.TxDecoder, ctx sdk.Context, handler sdk.AnteHandler, txs [][]byte) ([][]byte, sdk.Context) {
	n := 0
	msgSendTransactionCount := 0
	for _, tx := range txs {
		sdkTx, err := dec(tx)
		if err != nil {
			logger.Error("decoding already checked transaction", "tx", tmbytes.HexBytes(coretypes.Tx(tx).Hash()), "error", err)
			continue
		}
		msgTypes := msgTypes(sdkTx)
		if count := countOccurrence(msgTypes, sdk.MsgTypeURL(&types.MsgSend{})); count != 0 {
			if msgSendTransactionCount+count > v3consts.MsgSendTransactionCap {
				logger.Debug("skipping tx because the msg send transaction cap was reached", "tx", tmbytes.HexBytes(coretypes.Tx(tx).Hash()))
				continue
			}
			msgSendTransactionCount += count
		}

		ctx, err = handler(ctx, sdkTx, false)
		// either the transaction is invalid (ie incorrect nonce) and we
		// simply want to remove this tx, or we're catching a panic from one
		// of the anteHanders which is logged.
		if err != nil {
			logger.Error(
				"filtering already checked transaction",
				"tx", tmbytes.HexBytes(coretypes.Tx(tx).Hash()),
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
// and removes transactions that return an error. Panics are caught by the checkTxValidity
// function used to apply the ante handler.
func filterBlobTxs(logger log.Logger, dec sdk.TxDecoder, ctx sdk.Context, handler sdk.AnteHandler, txs []*tx.BlobTx) ([]*tx.BlobTx, sdk.Context) {
	n := 0
	pfbTransactionCount := 0
	for _, tx := range txs {
		sdkTx, err := dec(tx.Tx)
		if err != nil {
			logger.Error("decoding already checked blob transaction", "tx", tmbytes.HexBytes(coretypes.Tx(tx.Tx).Hash()), "error", err)
			continue
		}
		msgTypes := msgTypes(sdkTx)
		if count := countOccurrence(msgTypes, sdk.MsgTypeURL(&types2.MsgPayForBlobs{})); count != 0 {
			if pfbTransactionCount+count > v3consts.PFBTransactionCap {
				logger.Debug("skipping tx because the msg pfb transaction cap was reached", "tx", tmbytes.HexBytes(coretypes.Tx(tx.Tx).Hash()))
				continue
			}
			pfbTransactionCount += count
		}
		ctx, err = handler(ctx, sdkTx, false)
		// either the transaction is invalid (ie incorrect nonce) and we
		// simply want to remove this tx, or we're catching a panic from one
		// of the anteHanders which is logged.
		if err != nil {
			logger.Error(
				"filtering already checked blob transaction", "tx", tmbytes.HexBytes(coretypes.Tx(tx.Tx).Hash()), "error", err,
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

// countOccurrence takes a strings slice and counts the number
// of time the provided item exists in that slice.
func countOccurrence(slice []string, item string) int {
	count := 0
	for _, v := range slice {
		if v == item {
			count++
		}
	}
	return count
}

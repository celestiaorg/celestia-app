package app

import (
	"github.com/cosmos/cosmos-sdk/client"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/keeper"
	"github.com/tendermint/tendermint/libs/log"
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

// filterForValidPFBSignature verifies the signatures of the provided PFB transactions. If it is invalid, it
// drops the transaction.
func filterForValidPFBSignature(ctx sdk.Context, accountKeeper *keeper.AccountKeeper, txConfig client.TxConfig, txs [][]byte) [][]byte {
	normalTxs, blobTxs := separateTxs(txConfig, txs)

	// increment the sequences of the standard cosmos-sdk transactions. Panics
	// from the anteHandler are caught and logged.
	seqHandler := incrementSequenceAnteHandler(accountKeeper)

	normalTxs, ctx = filterStdTxs(ctx.Logger(), txConfig.TxDecoder(), ctx, seqHandler, normalTxs)

	// check the signatures and increment the sequences of the blob txs,
	// and filter out any that fail. Panics from the anteHandler are caught and
	// logged.
	svHandler := sigVerifyAnteHandler(accountKeeper, txConfig)
	blobTxs, _ = filterBlobTxs(ctx.Logger(), txConfig.TxDecoder(), ctx, svHandler, blobTxs)

	return append(normalTxs, encodeBlobTxs(blobTxs)...)
}

// filterStdTxs applies the provided antehandler to each transaction and removes
// transactions that return an error. Panics are caught by the checkTxValidity
// function used to apply the ante handler.
func filterStdTxs(logger log.Logger, dec sdk.TxDecoder, ctx sdk.Context, handler sdk.AnteHandler, txs [][]byte) ([][]byte, sdk.Context) {
	n := 0
	var err error
	for _, tx := range txs {
		ctx, err = checkTxValidity(logger, dec, ctx, handler, tx)
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
func filterBlobTxs(logger log.Logger, dec sdk.TxDecoder, ctx sdk.Context, handler sdk.AnteHandler, txs []tmproto.BlobTx) ([]tmproto.BlobTx, sdk.Context) {
	n := 0
	var err error
	for _, tx := range txs {
		ctx, err = checkTxValidity(logger, dec, ctx, handler, tx.Tx)
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

func checkTxValidity(logger log.Logger, dec sdk.TxDecoder, ctx sdk.Context, handler sdk.AnteHandler, tx []byte) (sdk.Context, error) {
	// catch panics from anteHandlers
	defer func() {
		if r := recover(); r != nil {
			err := recoverHandler(r)
			if err != nil {
				logger.Error(err.Error())
			}
		}
	}()

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

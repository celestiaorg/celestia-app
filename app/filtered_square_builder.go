package app

import (
	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	fibretypes "github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	square "github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/tx"
	tmbytes "github.com/cometbft/cometbft/libs/bytes"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// FilteredSquareBuilder filters txs and blobs using a copy of the state and tx validity
// rules before adding it the square.
type FilteredSquareBuilder struct {
	handler  sdk.AnteHandler
	txConfig client.TxConfig
	builder  *square.Builder
}

func NewFilteredSquareBuilder(
	handler sdk.AnteHandler,
	txConfig client.TxConfig,
	maxSquareSize,
	subtreeRootThreshold int,
) (*FilteredSquareBuilder, error) {
	builder, err := square.NewBuilder(maxSquareSize, subtreeRootThreshold)
	if err != nil {
		return nil, err
	}
	return &FilteredSquareBuilder{
		handler:  handler,
		txConfig: txConfig,
		builder:  builder,
	}, nil
}

func (fsb *FilteredSquareBuilder) Build() (square.Square, error) {
	return fsb.builder.Export()
}

func (fsb *FilteredSquareBuilder) Builder() *square.Builder {
	return fsb.builder
}

func (fsb *FilteredSquareBuilder) Fill(ctx sdk.Context, txs [][]byte) [][]byte {
	logger := ctx.Logger().With("app/filtered-square-builder")

	// note that there is an additional filter step for tx size of raw txs here
	normalTxs, blobTxs, payForFibreTxs := separateTxs(fsb.txConfig, txs)

	var (
		sdkMessageCount = 0
		pfbMessageCount = 0
		dec             = fsb.txConfig.TxDecoder()
		n               = 0
		m               = 0
	)

	for _, tx := range normalTxs {
		sdkTx, err := dec(tx)
		if err != nil {
			logger.Error("decoding already checked transaction", "tx", tmbytes.HexBytes(coretypes.Tx(tx).Hash()), "error", err)
			continue
		}

		// Set the tx size on the context before calling the AnteHandler
		ctx = ctx.WithTxBytes(tx)

		msgTypes := msgTypes(sdkTx)
		if sdkMessageCount+len(sdkTx.GetMsgs()) > appconsts.MaxSDKMessages {
			logger.Debug("skipping tx because the max SDK message count was reached", "tx", tmbytes.HexBytes(coretypes.Tx(tx).Hash()))
			continue
		}

		if !fsb.builder.AppendTx(tx) {
			logger.Debug("skipping tx because it was too large to fit in the square", "tx", tmbytes.HexBytes(coretypes.Tx(tx).Hash()))
			continue
		}

		ctx, err = fsb.handler(ctx, sdkTx, false)
		// either the transaction is invalid (ie incorrect nonce) and we
		// simply want to remove this tx, or we're catching a panic from one
		// of the anteHandlers which is logged.
		if err != nil {
			logger.Error(
				"filtering already checked transaction",
				"tx", tmbytes.HexBytes(coretypes.Tx(tx).Hash()),
				"error", err,
				"msgs", msgTypes,
			)
			telemetry.IncrCounter(1, "prepare_proposal", "invalid_std_txs")
			err = fsb.builder.RevertLastTx()
			if err != nil {
				logger.Error("reverting last transaction", "error", err)
			}
			continue
		}

		sdkMessageCount += len(sdkTx.GetMsgs())
		normalTxs[n] = tx
		n++
	}

	for _, tx := range blobTxs {
		sdkTx, err := dec(tx.Tx)
		if err != nil {
			logger.Error("decoding already checked blob transaction", "tx", tmbytes.HexBytes(coretypes.Tx(tx.Tx).Hash()), "error", err)
			continue
		}

		// Set the tx size on the context before calling the AnteHandler
		ctx = ctx.WithTxBytes(tx.Tx)

		if pfbMessageCount+len(sdkTx.GetMsgs()) > appconsts.MaxPFBMessages {
			logger.Debug("skipping blob tx because the max pfb message count was reached", "tx", tmbytes.HexBytes(coretypes.Tx(tx.Tx).Hash()))
			continue
		}

		ok, err := fsb.builder.AppendBlobTx(tx)
		if err != nil {
			logger.Debug("skipping blob tx due to error", "tx", tmbytes.HexBytes(coretypes.Tx(tx.Tx).Hash()), "err", err)
			continue
		}
		if !ok {
			logger.Debug("skipping tx because it was too large to fit in the square", "tx", tmbytes.HexBytes(coretypes.Tx(tx.Tx).Hash()))
			continue
		}

		ctx, err = fsb.handler(ctx, sdkTx, false)
		// either the transaction is invalid (ie incorrect nonce) and we
		// simply want to remove this tx, or we're catching a panic from one
		// of the anteHandlers which is logged.
		if err != nil {
			logger.Error(
				"filtering already checked blob transaction", "tx", tmbytes.HexBytes(coretypes.Tx(tx.Tx).Hash()), "error", err,
			)
			telemetry.IncrCounter(1, "prepare_proposal", "invalid_blob_txs")
			err = fsb.builder.RevertLastBlobTx()
			if err != nil {
				logger.Error("reverting last blob transaction failed", "error", err)
			}
			continue
		}

		pfbMessageCount += len(sdkTx.GetMsgs())
		blobTxs[m] = tx
		m++
	}

	// Process pay-for-fibre transactions: synthesize system blob, validate, append to builder.
	// Plain SDK tx bytes are returned unchanged so that the tx hash is stable: the hash the
	// client used to submit the tx is the same hash committed in the block, allowing ConfirmTx
	// to work.
	var pffMessageCount int
	fibreTxs := make([][]byte, 0, len(payForFibreTxs))
	for _, rawTx := range payForFibreTxs {
		// TryParseFibreTx parses the MsgPayForFibre proto fields and builds the system blob.
		// separateTxs guarantees rawTx contains exactly one MsgPayForFibre, so fibreTx is always non-nil.
		fibreTx, err := tx.TryParseFibreTx(rawTx)
		if err != nil {
			logger.Error("synthesizing fibre tx", "tx", tmbytes.HexBytes(coretypes.Tx(rawTx).Hash()), "error", err)
			continue
		}

		sdkTx, err := dec(rawTx)
		if err != nil {
			logger.Error("decoding pay-for-fibre transaction", "tx", tmbytes.HexBytes(coretypes.Tx(rawTx).Hash()), "error", err)
			continue
		}

		if pffMessageCount+len(sdkTx.GetMsgs()) > appconsts.MaxPayForFibreMessages {
			logger.Debug("skipping pay-for-fibre tx because the max PayForFibre message count was reached", "tx", tmbytes.HexBytes(coretypes.Tx(rawTx).Hash()))
			continue
		}

		ctx = ctx.WithTxBytes(rawTx)

		ok, err := fsb.builder.AppendFibreTx(fibreTx)
		if err != nil {
			logger.Error("appending pay-for-fibre transaction to builder", "tx", tmbytes.HexBytes(coretypes.Tx(rawTx).Hash()), "error", err)
			continue
		}
		if !ok {
			logger.Debug("skipping pay-for-fibre tx because it was too large to fit in the square", "tx", tmbytes.HexBytes(coretypes.Tx(rawTx).Hash()))
			continue
		}

		ctx, err = fsb.handler(ctx, sdkTx, false)
		if err != nil {
			logger.Error(
				"filtering already checked pay-for-fibre transaction",
				"tx", tmbytes.HexBytes(coretypes.Tx(rawTx).Hash()),
				"error", err,
				"msgs", msgTypes(sdkTx),
			)
			telemetry.IncrCounter(1, "prepare_proposal", "invalid_pay_for_fibre_txs")
			if revertErr := fsb.builder.RevertLastPayForFibreTx(); revertErr != nil {
				logger.Error("reverting last pay-for-fibre transaction", "error", revertErr)
			}
			continue
		}

		pffMessageCount += len(sdkTx.GetMsgs())
		fibreTxs = append(fibreTxs, rawTx)
	}

	kept := make([][]byte, 0, n+m+len(fibreTxs))
	kept = append(kept, normalTxs[:n]...)
	kept = append(kept, encodeBlobTxs(blobTxs[:m])...)
	kept = append(kept, fibreTxs...)
	return kept
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

// separateTxs decodes raw tendermint txs into normal, blob, and pay-for-fibre txs.
// This function filters out:
//   - transactions that exceed MaxTxSize
//   - transactions that fail SDK decoding
//   - transactions containing MsgPayForFibre mixed with other messages
//   - transactions containing more than one MsgPayForFibre
func separateTxs(txConfig client.TxConfig, rawTxs [][]byte) (normalTxs [][]byte, blobTxs []*tx.BlobTx, payForFibreTxs [][]byte) {
	normalTxs = make([][]byte, 0, len(rawTxs))
	blobTxs = make([]*tx.BlobTx, 0, len(rawTxs))
	payForFibreTxs = make([][]byte, 0, len(rawTxs))
	dec := txConfig.TxDecoder()

	for _, rawTx := range rawTxs {
		// this check in theory shouldn't get hit, as txs should be filtered
		// in CheckTx. However in tests we're inserting too large of txs
		// therefore also filter here.
		if len(rawTx) > appconsts.MaxTxSize {
			continue
		}

		bTx, isBlob, err := tx.UnmarshalBlobTx(rawTx)
		if isBlob {
			if err != nil {
				panic(err)
			}
			blobTxs = append(blobTxs, bTx)
			continue
		}

		sdkTx, err := dec(rawTx)
		if err != nil {
			// Skip txs that fail decoding. ProcessProposal rejects
			// undecodable txs, so there is no reason to include them.
			continue
		}

		pffCount := countMsgPayForFibre(sdkTx)
		if pffCount == 1 && len(sdkTx.GetMsgs()) == 1 {
			// A valid PayForFibre tx must contain exactly one message: the MsgPayForFibre.
			// This is consistent with BlobTx which also requires exactly one MsgPayForBlobs.
			payForFibreTxs = append(payForFibreTxs, rawTx)
			continue
		}
		if pffCount > 0 {
			// Skip invalid txs: multiple MsgPayForFibre or MsgPayForFibre mixed with other messages.
			continue
		}

		normalTxs = append(normalTxs, rawTx)
	}
	return normalTxs, blobTxs, payForFibreTxs
}

// countMsgPayForFibre returns the number of MsgPayForFibre messages in a transaction.
func countMsgPayForFibre(sdkTx sdk.Tx) int {
	count := 0
	for _, msg := range sdkTx.GetMsgs() {
		if _, ok := msg.(*fibretypes.MsgPayForFibre); ok {
			count++
		}
	}
	return count
}

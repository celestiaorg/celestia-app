package app

import (
	"cosmossdk.io/log"
	tmbytes "github.com/cometbft/cometbft/libs/bytes"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"

	square "github.com/celestiaorg/go-square/v2"
	"github.com/celestiaorg/go-square/v2/tx"

	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
)

// FilteredBuilder filters txs and blobs using a copy of the state and tx validity
// rules before adding it the square.
type FilteredBuilder struct {
	logger   log.Logger
	ctx      sdk.Context
	handler  sdk.AnteHandler
	txConfig client.TxConfig
	builder  *square.Builder
}

func NewFilteredBuilder(
	logger log.Logger,
	ctx sdk.Context,
	handler sdk.AnteHandler,
	txConfig client.TxConfig,
	maxSquareSize,
	subtreeRootThreshold int,
) (*FilteredBuilder, error) {
	builder, err := square.NewBuilder(maxSquareSize, subtreeRootThreshold)
	if err != nil {
		return nil, err
	}
	return &FilteredBuilder{
		logger:   logger,
		ctx:      ctx,
		handler:  handler,
		txConfig: txConfig,
		builder:  builder,
	}, nil
}

func (fb *FilteredBuilder) Build() (square.Square, error) {
	return fb.builder.Export()
}

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

func (fb *FilteredBuilder) Fill(txs [][]byte) [][]byte {
	normalTxs, blobTxs := separateTxs(fb.txConfig, txs)

	var (
		nonPFBMessageCount = 0
		pfbMessageCount    = 0
		dec                = fb.txConfig.TxDecoder()
		n                  = 0
		m                  = 0
	)

	for _, tx := range normalTxs {
		sdkTx, err := dec(tx)
		if err != nil {
			fb.logger.Error("decoding already checked transaction", "tx", tmbytes.HexBytes(coretypes.Tx(tx).Hash()), "error", err)
			continue
		}

		if !fb.builder.AppendTx(tx) {
			continue
		}

		// Set the tx size on the context before calling the AnteHandler
		fb.ctx = fb.ctx.WithTxBytes(tx)

		msgTypes := msgTypes(sdkTx)
		if nonPFBMessageCount+len(sdkTx.GetMsgs()) > appconsts.MaxNonPFBMessages {
			fb.logger.Debug("skipping tx because the max non PFB message count was reached", "tx", tmbytes.HexBytes(coretypes.Tx(tx).Hash()))
			continue
		}
		nonPFBMessageCount += len(sdkTx.GetMsgs())

		fb.ctx, err = fb.handler(fb.ctx, sdkTx, false)
		// either the transaction is invalid (ie incorrect nonce) and we
		// simply want to remove this tx, or we're catching a panic from one
		// of the anteHandlers which is logged.
		if err != nil {
			fb.logger.Error(
				"filtering already checked transaction",
				"tx", tmbytes.HexBytes(coretypes.Tx(tx).Hash()),
				"error", err,
				"msgs", msgTypes,
			)
			telemetry.IncrCounter(1, "prepare_proposal", "invalid_std_txs")
			err = fb.builder.RevertLastTx()
			if err != nil {
				fb.logger.Error("reverting last transaction", "error", err)
			}
			continue
		}

		normalTxs[n] = tx
		n++
	}

	for _, tx := range blobTxs {
		sdkTx, err := dec(tx.Tx)
		if err != nil {
			fb.logger.Error("decoding already checked blob transaction", "tx", tmbytes.HexBytes(coretypes.Tx(tx.Tx).Hash()), "error", err)
			continue
		}

		// Set the tx size on the context before calling the AnteHandler
		fb.ctx = fb.ctx.WithTxBytes(tx.Tx)

		if pfbMessageCount+len(sdkTx.GetMsgs()) > appconsts.MaxPFBMessages {
			fb.logger.Debug("skipping tx because the max pfb message count was reached", "tx", tmbytes.HexBytes(coretypes.Tx(tx.Tx).Hash()))
			continue
		}

		if !fb.builder.AppendBlobTx(tx) {
			continue
		}

		pfbMessageCount += len(sdkTx.GetMsgs())

		fb.ctx, err = fb.handler(fb.ctx, sdkTx, false)
		// either the transaction is invalid (ie incorrect nonce) and we
		// simply want to remove this tx, or we're catching a panic from one
		// of the anteHandlers which is logged.
		if err != nil {
			fb.logger.Error(
				"filtering already checked blob transaction", "tx", tmbytes.HexBytes(coretypes.Tx(tx.Tx).Hash()), "error", err,
			)
			telemetry.IncrCounter(1, "prepare_proposal", "invalid_blob_txs")
			err = fb.builder.RevertLastBlobTx()
			if err != nil {
				fb.logger.Error("reverting last blob transaction failed", "error", err)
			}
			continue
		}

		blobTxs[m] = tx
		m++
	}

	kept := make([][]byte, 0, m+n)
	kept = append(kept, normalTxs[:n]...)
	kept = append(kept, encodeBlobTxs(blobTxs[:m])...)
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

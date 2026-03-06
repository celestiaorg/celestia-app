package app

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	fibretypes "github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	square "github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
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
		nonPFBMessageCount = 0
		pfbMessageCount    = 0
		dec                = fsb.txConfig.TxDecoder()
		n                  = 0
		m                  = 0
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
		if nonPFBMessageCount+len(sdkTx.GetMsgs()) > appconsts.MaxNonPFBMessages {
			logger.Debug("skipping tx because the max non PFB message count was reached", "tx", tmbytes.HexBytes(coretypes.Tx(tx).Hash()))
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

		nonPFBMessageCount += len(sdkTx.GetMsgs())
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

	// Process pay-for-fibre transactions: validate, create system blob, wrap as FibreTx.
	// Entries in payForFibreTxs are either already-wrapped FibreTx bytes (submitted
	// by the fibre client directly) or plain SDK MsgPayForFibre bytes (submitted by
	// legacy clients).
	fibreTxs := make([][]byte, 0, len(payForFibreTxs))
	for _, rawTx := range payForFibreTxs {
		// Check if the entry is already a FibreTx (submitted by the fibre client).
		existingFibreTx, isAlreadyFibreTx, _ := tx.UnmarshalFibreTx(rawTx)

		var sdkTxBytes []byte
		var marshaledFibreTx []byte
		var fibreTxToAppend *tx.FibreTx

		if isAlreadyFibreTx {
			// Already wrapped: extract inner SDK tx for ante handler validation.
			sdkTxBytes = existingFibreTx.Tx
			marshaledFibreTx = rawTx
			fibreTxToAppend = existingFibreTx
		} else {
			// Plain SDK MsgPayForFibre: create the system blob and marshal.
			sdkTxBytes = rawTx

			sdkTx, err := dec(sdkTxBytes)
			if err != nil {
				logger.Error("decoding pay-for-fibre transaction", "tx", tmbytes.HexBytes(coretypes.Tx(rawTx).Hash()), "error", err)
				continue
			}

			// separateTxs guarantees rawTx contains MsgPayForFibre, so the bool is safe to ignore.
			msgPayForFibre, _ := extractMsgPayForFibre(sdkTx)
			systemBlob, err := createSystemBlobForPayForFibre(msgPayForFibre)
			if err != nil {
				logger.Error("creating system blob for pay-for-fibre transaction", "tx", tmbytes.HexBytes(coretypes.Tx(rawTx).Hash()), "error", err)
				continue
			}

			// Marshal before appending so that an encoding failure requires no builder revert.
			marshaledFibreTx, err = tx.MarshalFibreTx(rawTx, systemBlob)
			if err != nil {
				logger.Error("marshaling fibre tx", "tx", tmbytes.HexBytes(coretypes.Tx(rawTx).Hash()), "error", err)
				continue
			}

			fibreTxToAppend = &tx.FibreTx{Tx: rawTx, SystemBlob: systemBlob}
		}

		sdkTx, err := dec(sdkTxBytes)
		if err != nil {
			logger.Error("decoding inner SDK tx for pay-for-fibre", "tx", tmbytes.HexBytes(coretypes.Tx(rawTx).Hash()), "error", err)
			continue
		}

		ctx = ctx.WithTxBytes(sdkTxBytes)

		ok, err := fsb.builder.AppendFibreTx(fibreTxToAppend)
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

		fibreTxs = append(fibreTxs, marshaledFibreTx)
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
// This function also filters out transactions that exceed MaxTxSize.
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

		// Already-wrapped FibreTx (submitted by the fibre client directly) is
		// placed in payForFibreTxs. Fill detects the wrapping via UnmarshalFibreTx
		// and passes it through without re-wrapping.
		_, isFibre, err := tx.UnmarshalFibreTx(rawTx)
		if isFibre {
			if err != nil {
				panic(err)
			}
			payForFibreTxs = append(payForFibreTxs, rawTx)
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
			normalTxs = append(normalTxs, rawTx)
			continue
		}

		if _, isPayForFibre := extractMsgPayForFibre(sdkTx); isPayForFibre {
			payForFibreTxs = append(payForFibreTxs, rawTx)
			continue
		}

		normalTxs = append(normalTxs, rawTx)
	}
	return normalTxs, blobTxs, payForFibreTxs
}

// extractMsgPayForFibre returns the first MsgPayForFibre from a transaction's
// messages, and true if found; otherwise nil and false.
func extractMsgPayForFibre(sdkTx sdk.Tx) (*fibretypes.MsgPayForFibre, bool) {
	for _, msg := range sdkTx.GetMsgs() {
		if pff, ok := msg.(*fibretypes.MsgPayForFibre); ok {
			return pff, true
		}
	}
	return nil, false
}

// createSystemBlobForPayForFibre creates the system-level V2 blob that
// accompanies a MsgPayForFibre in the square.
func createSystemBlobForPayForFibre(msg *fibretypes.MsgPayForFibre) (*share.Blob, error) {
	namespaceBytes := msg.PaymentPromise.Namespace
	if len(namespaceBytes) != share.NamespaceSize {
		return nil, fmt.Errorf("invalid namespace size: expected %d bytes, got %d", share.NamespaceSize, len(namespaceBytes))
	}
	ns, err := share.NewNamespaceFromBytes(namespaceBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to create namespace: %w", err)
	}

	signerAddr, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return nil, fmt.Errorf("failed to decode signer address: %w", err)
	}
	if len(signerAddr) != share.SignerSize {
		return nil, fmt.Errorf("invalid signer size: expected %d bytes, got %d", share.SignerSize, len(signerAddr))
	}

	commitment := msg.PaymentPromise.Commitment
	if len(commitment) != share.FibreCommitmentSize {
		return nil, fmt.Errorf("invalid commitment size: expected %d bytes, got %d", share.FibreCommitmentSize, len(commitment))
	}

	blob, err := share.NewV2Blob(ns, msg.PaymentPromise.BlobVersion, commitment, signerAddr.Bytes())
	if err != nil {
		return nil, fmt.Errorf("failed to create V2 blob: %w", err)
	}
	return blob, nil
}

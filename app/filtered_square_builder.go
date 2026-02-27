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

		added, err := fsb.builder.AppendBlobTx(tx)
		if err != nil {
			logger.Error("appending blob tx to square builder", "tx", tmbytes.HexBytes(coretypes.Tx(tx.Tx).Hash()), "error", err)
			continue
		}
		if !added {
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

	payForFibreTxCount := 0

	for _, tx := range payForFibreTxs {
		sdkTx, err := dec(tx)
		if err != nil {
			logger.Error("decoding already checked pay-for-fibre transaction", "tx", tmbytes.HexBytes(coretypes.Tx(tx).Hash()), "error", err)
			continue
		}

		// Set the tx size on the context before calling the AnteHandler
		ctx = ctx.WithTxBytes(tx)

		msgTypes := msgTypes(sdkTx)

		// Append pay-for-fibre transaction to builder (will be routed to PayForFibreNamespace in Export)
		if !fsb.builder.AppendPayForFibreTx(tx) {
			logger.Debug("skipping pay-for-fibre tx because it was too large to fit in the square", "tx", tmbytes.HexBytes(coretypes.Tx(tx).Hash()))
			continue
		}

		ctx, err = fsb.handler(ctx, sdkTx, false)
		// either the transaction is invalid (ie incorrect nonce) and we
		// simply want to remove this tx, or we're catching a panic from one
		// of the anteHandlers which is logged.
		if err != nil {
			logger.Error(
				"filtering already checked pay-for-fibre transaction",
				"tx", tmbytes.HexBytes(coretypes.Tx(tx).Hash()),
				"error", err,
				"msgs", msgTypes,
			)
			telemetry.IncrCounter(1, "prepare_proposal", "invalid_pay_for_fibre_txs")
			err = fsb.builder.RevertLastPayForFibreTx()
			if err != nil {
				logger.Error("reverting last pay-for-fibre transaction", "error", err)
			}
			continue
		}

		// Generate and add system-level blob for this MsgPayForFibre transaction
		msgPayForFibre, hasPayForFibre := extractMsgPayForFibre(sdkTx)
		if hasPayForFibre {
			txHash := coretypes.Tx(tx).Hash()

			// Create system blob
			systemBlob, err := createSystemBlobForPayForFibre(msgPayForFibre)
			if err != nil {
				logger.Error(
					"failed to create system blob for pay-for-fibre transaction",
					"tx", tmbytes.HexBytes(txHash),
					"error", err,
				)
				telemetry.IncrCounter(1, "prepare_proposal", "failed_system_blob_creation")
				// Revert the transaction that was already appended
				if revertErr := fsb.builder.RevertLastPayForFibreTx(); revertErr != nil {
					logger.Error("reverting last pay-for-fibre transaction after system blob creation failure", "error", revertErr)
				}
				continue
			}

			// Add system blob to builder
			added, err := fsb.builder.AppendSystemBlob(systemBlob)
			if err != nil {
				logger.Error(
					"error appending system blob for pay-for-fibre tx",
					"tx", tmbytes.HexBytes(txHash),
					"error", err,
				)
				if revertErr := fsb.builder.RevertLastPayForFibreTx(); revertErr != nil {
					logger.Error("reverting last pay-for-fibre transaction after system blob error", "error", revertErr)
				}
				continue
			}
			if !added {
				logger.Debug(
					"skipping pay-for-fibre tx because system blob was too large to fit in the square",
					"tx", tmbytes.HexBytes(txHash),
				)
				// Revert the transaction that was already appended
				if revertErr := fsb.builder.RevertLastPayForFibreTx(); revertErr != nil {
					logger.Error("reverting last pay-for-fibre transaction after system blob addition failure", "error", revertErr)
				}
				continue
			}
		}

		payForFibreTxs[payForFibreTxCount] = tx
		payForFibreTxCount++
	}

	kept := make([][]byte, 0, m+n+payForFibreTxCount)
	kept = append(kept, normalTxs[:n]...)
	kept = append(kept, encodeBlobTxs(blobTxs[:m])...)
	kept = append(kept, payForFibreTxs[:payForFibreTxCount]...)
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
func separateTxs(txConfig client.TxConfig, rawTxs [][]byte) (normalTxs [][]byte, blobTxs []*tx.BlobTx, payForFibreTxs [][]byte) {
	normalTxs = make([][]byte, 0, len(rawTxs))
	blobTxs = make([]*tx.BlobTx, 0, len(rawTxs))
	payForFibreTxs = make([][]byte, 0, len(rawTxs))
	dec := txConfig.TxDecoder()

	for _, rawTx := range rawTxs {
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

		// Decode the transaction
		sdkTx, err := dec(rawTx)
		if err != nil {
			normalTxs = append(normalTxs, rawTx)
			continue
		}

		// Check if this is a pay-for-fibre transaction
		if _, hasPayForFibre := extractMsgPayForFibre(sdkTx); hasPayForFibre {
			payForFibreTxs = append(payForFibreTxs, rawTx)
			continue
		}
		normalTxs = append(normalTxs, rawTx)
	}
	return normalTxs, blobTxs, payForFibreTxs
}

// payForFibreHandler implements square.PayForFibreHandler for celestia-app.
type payForFibreHandler struct {
	txConfig client.TxConfig
}

// NewPayForFibreHandler creates a new PayForFibreHandler for celestia-app.
func NewPayForFibreHandler(txConfig client.TxConfig) square.PayForFibreHandler {
	return &payForFibreHandler{txConfig: txConfig}
}

// IsPayForFibreTx returns true if the transaction contains a MsgPayForFibre message.
func (h *payForFibreHandler) IsPayForFibreTx(tx []byte) bool {
	sdkTx, err := h.txConfig.TxDecoder()(tx)
	if err != nil {
		return false
	}
	_, hasPayForFibre := extractMsgPayForFibre(sdkTx)
	return hasPayForFibre
}

// CreateSystemBlob creates a system blob from a PayForFibre transaction.
func (h *payForFibreHandler) CreateSystemBlob(tx []byte) (*share.Blob, error) {
	sdkTx, err := h.txConfig.TxDecoder()(tx)
	if err != nil {
		return nil, fmt.Errorf("failed to decode transaction: %w", err)
	}
	msgPayForFibre, hasPayForFibre := extractMsgPayForFibre(sdkTx)
	if !hasPayForFibre {
		return nil, fmt.Errorf("transaction does not contain MsgPayForFibre")
	}
	return createSystemBlobForPayForFibre(msgPayForFibre)
}

// extractMsgPayForFibre extracts MsgPayForFibre from a transaction's messages.
func extractMsgPayForFibre(sdkTx sdk.Tx) (*fibretypes.MsgPayForFibre, bool) {
	msgs := sdkTx.GetMsgs()
	for _, msg := range msgs {
		if pff, ok := msg.(*fibretypes.MsgPayForFibre); ok {
			return pff, true
		}
	}
	return nil, false
}

// createSystemBlobForPayForFibre creates a system-level blob for a MsgPayForFibre message.
func createSystemBlobForPayForFibre(msg *fibretypes.MsgPayForFibre) (*share.Blob, error) {
	namespaceBytes := msg.PaymentPromise.Namespace
	if len(namespaceBytes) != share.NamespaceSize {
		return nil, fmt.Errorf("invalid namespace size: expected %d bytes, got %d", share.NamespaceSize, len(namespaceBytes))
	}
	namespace, err := share.NewNamespaceFromBytes(namespaceBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to create namespace: %w", err)
	}

	signerAddr, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return nil, fmt.Errorf("failed to decode signer address: %w", err)
	}
	signerBytes := signerAddr.Bytes()
	if len(signerBytes) != share.SignerSize {
		return nil, fmt.Errorf("invalid signer size: expected %d bytes, got %d", share.SignerSize, len(signerBytes))
	}

	fibreBlobVersion := msg.PaymentPromise.BlobVersion
	commitment := msg.PaymentPromise.Commitment
	if len(commitment) != share.FibreCommitmentSize {
		return nil, fmt.Errorf("invalid commitment size: expected %d bytes, got %d", share.FibreCommitmentSize, len(commitment))
	}

	blob, err := share.NewV2Blob(namespace, fibreBlobVersion, commitment, signerBytes)
	if err != nil {
		return nil, fmt.Errorf("failed to create V2 blob: %w", err)
	}

	return blob, nil
}

// noOpPayForFibreHandler is a PayForFibreHandler that always returns false for
// IsPayForFibreTx. It is used when Fibre support is not enabled.
type noOpPayForFibreHandler struct{}

func (h *noOpPayForFibreHandler) IsPayForFibreTx(_ []byte) bool              { return false }
func (h *noOpPayForFibreHandler) CreateSystemBlob(_ []byte) (*share.Blob, error) { return nil, nil }

// NoOpPayForFibreHandler returns a PayForFibreHandler that treats no transactions
// as PayForFibre transactions.
func NoOpPayForFibreHandler() square.PayForFibreHandler {
	return &noOpPayForFibreHandler{}
}

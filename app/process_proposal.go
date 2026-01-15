package app

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"time"

	"cosmossdk.io/errors"
	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v7/app/ante"
	apperr "github.com/celestiaorg/celestia-app/v7/app/errors"
	"github.com/celestiaorg/celestia-app/v7/app/params"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v7/pkg/da"
	blobtypes "github.com/celestiaorg/celestia-app/v7/x/blob/types"
	feeaddresstypes "github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	blobtx "github.com/celestiaorg/go-square/v3/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const rejectedPropBlockLog = "Rejected proposal block:"

func (app *App) ProcessProposalHandler(ctx sdk.Context, req *abci.RequestProcessProposal) (resp *abci.ResponseProcessProposal, err error) {
	defer telemetry.MeasureSince(time.Now(), "process_proposal")
	// In the case of a panic resulting from an unexpected condition, it is
	// better for the liveness of the network to catch it, log an error, and
	// vote nil rather than crashing the node.
	defer func() {
		if err := recover(); err != nil {
			logInvalidPropBlock(app.Logger(), ctx.BlockHeader(), fmt.Sprintf("caught panic: %v", err))
			telemetry.IncrCounter(1, "process_proposal", "panics")
			resp = reject()
		}
	}()

	// Create the anteHandler that is used to check the validity of
	// transactions. All transactions need to be equally validated here
	// so that the nonce number is always correctly incremented (which
	// may affect the validity of future transactions).
	handler := ante.NewAnteHandler(
		app.AccountKeeper,
		app.BankKeeper,
		app.BlobKeeper,
		app.FeeGrantKeeper,
		app.GetTxConfig().SignModeHandler(),
		ante.DefaultSigVerificationGasConsumer,
		app.IBCKeeper,
		app.MinFeeKeeper,
		&app.CircuitKeeper,
		app.GovParamFilters(),
	)
	blockHeader := ctx.BlockHeader()

	// Strict validation for fee forward transactions:
	// If the fee address has a balance, the block MUST contain a valid fee forward tx as the first tx.
	// If no balance but fee forward tx present, reject the block.
	feeBalance := app.BankKeeper.GetBalance(ctx, feeaddresstypes.FeeAddress, params.BondDenom)
	hasBalance := !feeBalance.IsZero()

	if len(req.Txs) > 0 {
		firstTxIsFeeForward, feeForwardErr := app.isFeeForwardTx(req.Txs[0])
		if feeForwardErr != nil && hasBalance {
			// If we have balance and can't decode the first tx, that's invalid
			logInvalidPropBlockError(app.Logger(), blockHeader, "failed to decode first tx for fee forward check", feeForwardErr)
			return reject(), nil
		}

		if hasBalance {
			// Fee address has balance - MUST have valid fee forward tx as first tx
			if !firstTxIsFeeForward {
				logInvalidPropBlock(app.Logger(), blockHeader, "fee address has balance but first tx is not a fee forward tx")
				return reject(), nil
			}
			// Validate the fee forward tx
			if err := app.validateFeeForwardTx(ctx, req.Txs[0], feeBalance); err != nil {
				logInvalidPropBlockError(app.Logger(), blockHeader, "invalid fee forward tx", err)
				return reject(), nil
			}
		} else if firstTxIsFeeForward {
			// No balance but fee forward tx present - reject
			logInvalidPropBlock(app.Logger(), blockHeader, "fee forward tx present but fee address has no balance")
			return reject(), nil
		}
	} else if hasBalance {
		// No transactions but fee address has balance - reject
		logInvalidPropBlock(app.Logger(), blockHeader, "fee address has balance but block has no transactions")
		return reject(), nil
	}

	// iterate over all txs and ensure that all blobTxs are valid, PFBs are correctly signed, non
	// blobTxs have no PFBs present and all txs are less than or equal to the max tx size limit
	for idx, rawTx := range req.Txs {
		tx := rawTx

		// all txs must be less than or equal to the max tx size limit
		currentTxSize := len(tx)
		if currentTxSize > appconsts.MaxTxSize {
			logInvalidPropBlockError(app.Logger(), blockHeader, fmt.Sprintf("err with tx %d", idx), errors.Wrapf(apperr.ErrTxExceedsMaxSize, "tx size %d bytes is larger than the application's configured MaxTxSize of %d bytes", currentTxSize, appconsts.MaxTxSize))
			return reject(), nil
		}

		blobTx, isBlobTx, err := blobtx.UnmarshalBlobTx(rawTx)
		if isBlobTx {
			if err != nil {
				logInvalidPropBlockError(app.Logger(), blockHeader, fmt.Sprintf("err with blob tx %d", idx), err)
				return reject(), nil
			}
			tx = blobTx.Tx
		}
		sdkTx, err := app.encodingConfig.TxConfig.TxDecoder()(tx)

		// Set the tx bytes in the context for app version v3 and greater
		ctx = ctx.WithTxBytes(tx)

		if err != nil {
			// An error here means that a tx was included in the block that is not decodable.
			logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("tx %d is not decodable", idx))
			return reject(), nil
		}

		// handle non-blob transactions first
		if !isBlobTx {
			msgs := sdkTx.GetMsgs()

			_, has := hasPFB(msgs)
			if has {
				// A non-blob tx has a PFB, which is invalid
				logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("tx %d has PFB but is not a blob tx", idx))
				return reject(), nil
			}

			// we need to increment the sequence for every transaction so that
			// the signature check below is accurate. this error only gets hit
			// if the account in question doesn't exist.
			ctx, err = handler(ctx, sdkTx, false)
			if err != nil {
				logInvalidPropBlockError(app.Logger(), blockHeader, "failure to increment sequence", err)
				return reject(), nil
			}

			// we do not need to perform further checks on this transaction,
			// since it has no PFB
			continue
		}

		// validate the blobTx. This is the same validation used in CheckTx ensuring
		// - there is one PFB
		// - that each blob has a valid namespace
		// - that the sizes match
		// - that the namespaces match between blob and PFB
		// - that the share commitment is correct
		// If this tx was cached from CheckTx, we can skip the expensive
		// commitment verification since it was already validated. Otherwise, fall back to full validation.
		if _, err := app.ValidateBlobTxWithCache(blobTx); err != nil {
			logInvalidPropBlockError(app.Logger(), blockHeader, fmt.Sprintf("blob tx validation failed %d", idx), err)
			return reject(), nil
		}

		ctx, err = handler(ctx, sdkTx, false)
		if err != nil {
			logInvalidPropBlockError(app.Logger(), blockHeader, "ante handler validation failed", err)
			return reject(), nil
		}

	}

	eds, err := da.ConstructEDSWithTreePool(req.Txs, appconsts.Version, app.MaxEffectiveSquareSize(ctx), app.TreePool())
	if err != nil {
		logInvalidPropBlockError(app.Logger(), blockHeader, "failure to compute extended data square from transactions:", err)
		return reject(), nil
	}

	// Assert that the square size stated by the proposer is correct
	if uint64(eds.Width()) != req.SquareSize*2 {
		logInvalidPropBlock(app.Logger(), blockHeader, "proposed square size differs from calculated square size")
		return reject(), nil
	}

	dah, err := da.NewDataAvailabilityHeader(eds)
	if err != nil {
		logInvalidPropBlockError(app.Logger(), blockHeader, "failure to create new data availability header", err)
		return reject(), nil
	}

	// by comparing the hashes we know the computed IndexWrappers (with the share indexes of the PFB's blobs)
	// are identical and that square layout is consistent. This also means that the share commitment rules
	// have been followed and thus each blobs share commitment should be valid
	if !bytes.Equal(dah.Hash(), req.DataRootHash) {
		logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("proposed data root %X differs from calculated data root %X", req.DataRootHash, dah.Hash()))
		return reject(), nil
	}

	return accept(), nil
}

func hasPFB(msgs []sdk.Msg) (*blobtypes.MsgPayForBlobs, bool) {
	for _, msg := range msgs {
		if pfb, ok := msg.(*blobtypes.MsgPayForBlobs); ok {
			return pfb, true
		}
	}
	return nil, false
}

func logInvalidPropBlock(l log.Logger, h tmproto.Header, reason string) {
	l.Error(
		rejectedPropBlockLog,
		"reason",
		reason,
		"proposer",
		h.ProposerAddress,
	)
}

func logInvalidPropBlockError(l log.Logger, h tmproto.Header, reason string, err error) {
	l.Error(
		rejectedPropBlockLog,
		"reason",
		reason,
		"proposer",
		h.ProposerAddress,
		"err",
		err.Error(),
	)
}

func reject() *abci.ResponseProcessProposal {
	return &abci.ResponseProcessProposal{
		Status: abci.ResponseProcessProposal_REJECT,
	}
}

func accept() *abci.ResponseProcessProposal {
	return &abci.ResponseProcessProposal{
		Status: abci.ResponseProcessProposal_ACCEPT,
	}
}

// ValidateBlobTxWithCache validates a blob transaction, using cached validation results when possible.
// It returns (fromCache, error) where fromCache indicates if the validation was skipped using cache.
func (app *App) ValidateBlobTxWithCache(blobTx *blobtx.BlobTx) (bool, error) {
	exists := app.txCache.Exists(blobTx.Tx, blobTx.Blobs)
	if exists {
		if _, err := blobtypes.ValidateBlobTxSkipCommitment(app.encodingConfig.TxConfig, blobTx); err != nil {
			return true, err
		}
		return true, nil
	}

	if err := blobtypes.ValidateBlobTx(app.encodingConfig.TxConfig, blobTx, appconsts.SubtreeRootThreshold, appconsts.Version); err != nil {
		return false, err
	}
	return false, nil
}

// isFeeForwardTx checks if the given raw transaction bytes contain a MsgForwardFees message.
func (app *App) isFeeForwardTx(txBytes []byte) (bool, error) {
	sdkTx, err := app.encodingConfig.TxConfig.TxDecoder()(txBytes)
	if err != nil {
		return false, err
	}
	msgs := sdkTx.GetMsgs()
	if len(msgs) != 1 {
		return false, nil
	}
	_, ok := msgs[0].(*feeaddresstypes.MsgForwardFees)
	return ok, nil
}

// validateFeeForwardTx validates a fee forward transaction:
// - The fee must equal the expected fee balance
// - The proposer must match the block proposer
func (app *App) validateFeeForwardTx(ctx sdk.Context, txBytes []byte, expectedFee sdk.Coin) error {
	sdkTx, err := app.encodingConfig.TxConfig.TxDecoder()(txBytes)
	if err != nil {
		return fmt.Errorf("failed to decode tx: %w", err)
	}

	// Verify there's exactly one message and it's MsgForwardFees
	msgs := sdkTx.GetMsgs()
	if len(msgs) != 1 {
		return fmt.Errorf("fee forward tx must have exactly one message, got %d", len(msgs))
	}
	msg, ok := msgs[0].(*feeaddresstypes.MsgForwardFees)
	if !ok {
		return fmt.Errorf("message is not MsgForwardFees")
	}

	// Verify the proposer matches the block proposer
	blockProposer := ctx.BlockHeader().ProposerAddress
	msgProposer, err := hex.DecodeString(msg.Proposer)
	if err != nil {
		return fmt.Errorf("invalid proposer address encoding: %w", err)
	}
	if !bytes.Equal(blockProposer, msgProposer) {
		return fmt.Errorf("proposer %X does not match block proposer %X", msgProposer, blockProposer)
	}

	// Verify the fee equals the expected fee balance
	feeTx, ok := sdkTx.(sdk.FeeTx)
	if !ok {
		return fmt.Errorf("tx does not implement FeeTx")
	}
	fee := feeTx.GetFee()
	if len(fee) != 1 {
		return fmt.Errorf("fee forward tx must have exactly one fee coin, got %d", len(fee))
	}
	if !fee[0].Equal(expectedFee) {
		return fmt.Errorf("fee %s does not equal expected fee %s", fee[0], expectedFee)
	}

	return nil
}

package app

import (
	"bytes"
	"fmt"
	"time"

	"cosmossdk.io/errors"
	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v10/app/ante"
	apperr "github.com/celestiaorg/celestia-app/v10/app/errors"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/da"
	blobtypes "github.com/celestiaorg/celestia-app/v10/x/blob/types"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	squarev4 "github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
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
		app.FibreKeeper,
		app.pffSigCache,
	)
	blockHeader := ctx.BlockHeader()

	// Run the fibre BeginBlocker on the proposal branch, mirroring FinalizeBlock,
	// which pays out matured withdrawals and advances the freshness floor before
	// any tx. Pay-for-fibre settlement below must see that escrow state. The
	// branch is discarded, so nothing commits.
	if err := app.FibreKeeper.BeginBlocker(ctx); err != nil {
		logInvalidPropBlockError(app.Logger(), blockHeader, "failed to run fibre begin blocker on proposal branch", err)
		return reject(), nil
	}

	var (
		sdkMessageCount int
		pfbMessageCount int
		pffMessageCount int
		maxPFF          = appconsts.MaxPayForFibreMessages
	)

	// iterate over all txs and ensure that all blobTxs are valid, PFBs are correctly signed, non
	// blobTxs have no PFBs present and all txs are less than or equal to the max tx size limit
	for idx, rawTx := range req.Txs {
		sdkTxBytes := rawTx

		// all txs must be less than or equal to the max tx size limit
		currentTxSize := len(rawTx)
		if currentTxSize > appconsts.MaxTxSize {
			logInvalidPropBlockError(app.Logger(), blockHeader, fmt.Sprintf("err with tx %d", idx), errors.Wrapf(apperr.ErrTxExceedsMaxSize, "tx size %d bytes is larger than the application's configured MaxTxSize of %d bytes", currentTxSize, appconsts.MaxTxSize))
			return reject(), nil
		}

		// BlobTx is the most common special type; check it first.
		blobTx, isBlobTx, err := blobtx.UnmarshalBlobTx(rawTx)
		if isBlobTx {
			if err != nil {
				logInvalidPropBlockError(app.Logger(), blockHeader, fmt.Sprintf("err with blob tx %d", idx), err)
				return reject(), nil
			}
			if !blobTxIsCanonical(rawTx, blobTx) {
				logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("blob tx %d is not canonically encoded", idx))
				return reject(), nil
			}
			sdkTxBytes = blobTx.Tx
		}

		sdkTx, err := app.encodingConfig.TxConfig.TxDecoder()(sdkTxBytes)
		ctx = ctx.WithTxBytes(sdkTxBytes)

		if err != nil {
			// An error here means that a tx was included in the block that is not decodable.
			logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("tx %d is not decodable", idx))
			return reject(), nil
		}

		// Handle non-blob transactions. This also validates MsgPayForFibre txs
		// (plain SDK txs, not wrapped in BlobTx).
		if !isBlobTx {
			msgs := sdkTx.GetMsgs()

			_, has := hasPFB(msgs)
			if has {
				// A non-blob tx has a PFB, which is invalid
				logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("tx %d has PFB but is not a blob tx", idx))
				return reject(), nil
			}

			// Validate MsgPayForFibre constraints.
			if err := validatePayForFibreTxShape(sdkTx); err != nil {
				logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("tx %d: %s", idx, err))
				return reject(), nil
			}

			_, isPFF := payForFibreMsg(sdkTx)
			if isPFF {
				pffMessageCount++
				if maxPFF > 0 && pffMessageCount > maxPFF {
					logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("block exceeds max PayForFibre message count of %d", maxPFF))
					return reject(), nil
				}
			} else {
				sdkMessageCount += countExecutableMsgs(msgs)
				if sdkMessageCount > appconsts.MaxSDKMessages {
					logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("block exceeds max SDK message count of %d", appconsts.MaxSDKMessages))
					return reject(), nil
				}
			}

			// we need to increment the sequence for every transaction so that
			// the signature check below is accurate. this error only gets hit
			// if the account in question doesn't exist.
			ctx, err = handler(ctx, sdkTx, false)
			if err != nil {
				logInvalidPropBlockError(app.Logger(), blockHeader, "failure to increment sequence", err)
				return reject(), nil
			}

			// Settle after ante so later promises see the updated state and the tx
			// pays the same gas it would in FinalizeBlock. This ante pass is also
			// the consensus enforcement point for PFF validator signatures:
			// FinalizeBlock never re-verifies them (see
			// FibreSignatureVerificationDecorator), so removing it would let a
			// proposer settle promises without a validator quorum.
			if isPFF {
				if execErr := executeTxMsgs(ctx, sdkTx, app.MsgServiceRouter()); execErr != nil {
					logInvalidPropBlockError(app.Logger(), blockHeader, fmt.Sprintf("fibre settlement failed %d", idx), execErr)
					return reject(), nil
				}
			} else if containsFibreStateMsg(sdkTx) {
				// Replay fibre escrow effects in block order so later settlement
				// sees the FinalizeBlock balance. A failed message keeps the tx
				// (gas only), so don't reject.
				if execErr := executeTxMsgs(ctx, sdkTx, app.MsgServiceRouter()); execErr != nil {
					app.Logger().Debug("fibre state msg did not settle in proposal; keeping tx", "idx", idx, "err", execErr)
				}
			}

			// The non-blob path is complete; blob-specific checks below do not apply.
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

		pfbMessageCount += len(sdkTx.GetMsgs())
		if pfbMessageCount > appconsts.MaxPFBMessages {
			logInvalidPropBlock(app.Logger(), blockHeader, fmt.Sprintf("block exceeds max PFB message count of %d", appconsts.MaxPFBMessages))
			return reject(), nil
		}

		ctx, err = handler(ctx, sdkTx, false)
		if err != nil {
			logInvalidPropBlockError(app.Logger(), blockHeader, "ante handler validation failed", err)
			return reject(), nil
		}

	}

	// Classify txs (marking pay-for-fibre txs and synthesizing their system
	// blobs) before constructing the square; go-square no longer decodes
	// Cosmos SDK transactions itself.
	classifiedTxs, err := fibretypes.ClassifyTxs(req.Txs)
	if err != nil {
		logInvalidPropBlockError(app.Logger(), blockHeader, "failed to classify transactions:", err)
		return reject(), nil
	}
	dataSquare, err := squarev4.Construct(classifiedTxs, app.MaxEffectiveSquareSize(ctx), appconsts.SubtreeRootThreshold)
	if err != nil {
		logInvalidPropBlockError(app.Logger(), blockHeader, "failed to build data square:", err)
		return reject(), nil
	}

	eds, err := da.ExtendSharesWithTreePool(share.ToBytes(dataSquare), app.TreePool())
	if err != nil {
		logInvalidPropBlockError(app.Logger(), blockHeader, "failure to compute extended data square from transactions:", err)
		return reject(), nil
	}

	// Assert that the square size stated by the proposer is correct. Compare
	// the halved EDS width rather than the doubled proposer value: doubling an
	// attacker controlled uint64 wraps, so SquareSize and SquareSize+2^63 would
	// otherwise be indistinguishable.
	if uint64(eds.Width())/2 != req.SquareSize {
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

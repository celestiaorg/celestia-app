package app

import (
	"bytes"
	"fmt"
	"time"

	"cosmossdk.io/errors"
	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v6/app/ante"
	apperr "github.com/celestiaorg/celestia-app/v6/app/errors"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/pkg/da"
	blobtypes "github.com/celestiaorg/celestia-app/v6/x/blob/types"
	"github.com/celestiaorg/go-square/v2"
	"github.com/celestiaorg/go-square/v2/share"
	blobtx "github.com/celestiaorg/go-square/v2/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const rejectedPropBlockLog = "Rejected proposal block:"

// squareResult holds the result of square construction
type squareResult struct {
	dataSquare square.Square
	dah        da.DataAvailabilityHeader
	err        error
}

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

	// Run transaction validation and square construction in parallel
	txValidationCh := make(chan error, 1)
	squareConstructionCh := make(chan squareResult, 1)

	// Start transaction validation in parallel
	go func() {
		// iterate over all txs and ensure that all blobTxs are valid, PFBs are correctly signed, non
		// blobTxs have no PFBs present and all txs are less than or equal to the max tx size limit
		for idx, rawTx := range req.Txs {
			tx := rawTx

			// all txs must be less than or equal to the max tx size limit
			currentTxSize := len(tx)
			if currentTxSize > appconsts.MaxTxSize {
				txValidationCh <- errors.Wrapf(apperr.ErrTxExceedsMaxSize, "tx %d size %d bytes is larger than the application's configured MaxTxSize of %d bytes", idx, currentTxSize, appconsts.MaxTxSize)
			}

			blobTx, isBlobTx, err := blobtx.UnmarshalBlobTx(rawTx)
			if isBlobTx {
				if err != nil {
					txValidationCh <- fmt.Errorf("blob tx %d unmarshal failed: %w", idx, err)
				}
				tx = blobTx.Tx
			}
			sdkTx, err := app.encodingConfig.TxConfig.TxDecoder()(tx)

			// Set the tx bytes in the context for app version v3 and greater
			ctx = ctx.WithTxBytes(tx)

			if err != nil {
				// An error here means that a tx was included in the block that is not decodable.
				txValidationCh <- fmt.Errorf("tx %d is not decodable", idx)
			}

			// handle non-blob transactions first
			if !isBlobTx {
				msgs := sdkTx.GetMsgs()

				_, has := hasPFB(msgs)
				if has {
					// A non-blob tx has a PFB, which is invalid
					txValidationCh <- fmt.Errorf("tx %d has PFB but is not a blob tx", idx)
				}

				// we need to increment the sequence for every transaction so that
				// the signature check below is accurate. this error only gets hit
				// if the account in question doesn't exist.
				ctx, err = handler(ctx, sdkTx, false)
				if err != nil {
					txValidationCh <- fmt.Errorf("tx %d ante handler failed: %w", idx, err)
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
			if err := blobtypes.ValidateBlobTx(app.encodingConfig.TxConfig, blobTx, appconsts.SubtreeRootThreshold, appconsts.Version); err != nil {
				txValidationCh <- fmt.Errorf("blob tx %d validation failed: %w", idx, err)
			}

			// validated the PFB signature
			ctx, err = handler(ctx, sdkTx, false)
			if err != nil {
				txValidationCh <- fmt.Errorf("blob tx %d ante handler failed: %w", idx, err)
			}
		}

		txValidationCh <- err
	}()

	// Start square construction in parallel
	go func() {
		dataSquare, err := square.Construct(req.Txs, app.MaxEffectiveSquareSize(ctx), appconsts.SubtreeRootThreshold)
		if err != nil {
			squareConstructionCh <- squareResult{err: fmt.Errorf("data square construction failed: %w", err)}
		}

		eds, err := da.ExtendShares(share.ToBytes(dataSquare))
		if err != nil {
			squareConstructionCh <- squareResult{err: fmt.Errorf("erasure coding failed: %w", err)}
		}

		dah, err := da.NewDataAvailabilityHeader(eds)
		if err != nil {
			squareConstructionCh <- squareResult{err: fmt.Errorf("DAH creation failed: %w", err)}
		}

		squareConstructionCh <- squareResult{
			dataSquare: dataSquare,
			dah:        dah,
		}
	}()

	// Wait for both processes to complete
	txValidationErr := <-txValidationCh
	squareRes := <-squareConstructionCh

	// Check transaction validation result
	if txValidationErr != nil {
		logInvalidPropBlockError(app.Logger(), blockHeader, "transaction validation failed", txValidationErr)
		return reject(), nil
	}

	// Check square construction result
	if squareRes.err != nil {
		logInvalidPropBlockError(app.Logger(), blockHeader, "square construction failed", squareRes.err)
		return reject(), nil
	}

	// Assert that the square size stated by the proposer is correct
	if uint64(squareRes.dataSquare.Size()) != req.SquareSize {
		logInvalidPropBlock(app.Logger(), blockHeader, "proposed square size differs from calculated square size")
		return reject(), nil
	}

	dah := squareRes.dah

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

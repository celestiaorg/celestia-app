package app

import (
	"bytes"
	"fmt"
	"time"

	"cosmossdk.io/log"
	abci "github.com/cometbft/cometbft/abci/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"

	shares "github.com/celestiaorg/go-square/shares"
	square "github.com/celestiaorg/go-square/square"
	squarev2 "github.com/celestiaorg/go-square/v2"
	sharev2 "github.com/celestiaorg/go-square/v2/share"
	blobtx "github.com/celestiaorg/go-square/v2/tx"

	"github.com/celestiaorg/celestia-app/v4/app/ante"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/pkg/da"
	blobtypes "github.com/celestiaorg/celestia-app/v4/x/blob/types"
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

	// Create the anteHander that is used to check the validity of
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
		app.ParamsKeeper,
		&app.CircuitKeeper,
		app.BlockedParamsGovernance(),
	)
	blockHeader := ctx.BlockHeader()

	appVersion, err := app.AppVersion(ctx)
	if err != nil {
		logInvalidPropBlockError(app.Logger(), blockHeader, "failure to get app version", err)
		return reject(), nil
	}

	subtreeRootThreshold := appconsts.SubtreeRootThreshold(appVersion)

	// iterate over all txs and ensure that all blobTxs are valid, PFBs are correctly signed and non
	// blobTxs have no PFBs present
	for idx, rawTx := range req.Txs {
		tx := rawTx
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
		if err := blobtypes.ValidateBlobTx(app.encodingConfig.TxConfig, blobTx, subtreeRootThreshold, appVersion); err != nil {
			logInvalidPropBlockError(app.Logger(), blockHeader, fmt.Sprintf("invalid blob tx %d", idx), err)
			return reject(), nil
		}

		// validated the PFB signature
		ctx, err = handler(ctx, sdkTx, false)
		if err != nil {
			logInvalidPropBlockError(app.Logger(), blockHeader, "invalid PFB signature", err)
			return reject(), nil
		}

	}

	var dataSquareBytes [][]byte

	switch appVersion {
	case v4, v3:
		var dataSquare squarev2.Square
		dataSquare, err = squarev2.Construct(req.Txs, app.MaxEffectiveSquareSize(ctx), subtreeRootThreshold)
		dataSquareBytes = sharev2.ToBytes(dataSquare)
		// Assert that the square size stated by the proposer is correct
		if uint64(dataSquare.Size()) != req.SquareSize {
			logInvalidPropBlock(app.Logger(), blockHeader, "proposed square size differs from calculated square size")
			return reject(), nil
		}
	case v2, v1:
		var dataSquare square.Square
		dataSquare, err = square.Construct(req.Txs, app.MaxEffectiveSquareSize(ctx), subtreeRootThreshold)
		dataSquareBytes = shares.ToBytes(dataSquare)
		// Assert that the square size stated by the proposer is correct
		if uint64(dataSquare.Size()) != req.SquareSize {
			logInvalidPropBlock(app.Logger(), blockHeader, "proposed square size differs from calculated square size")
			return reject(), nil
		}
	default:
		logInvalidPropBlock(app.Logger(), blockHeader, "unsupported app version")
		return reject(), nil
	}
	if err != nil {
		logInvalidPropBlockError(app.Logger(), blockHeader, "failure to compute data square from transactions:", err)
		return reject(), nil
	}

	eds, err := da.ExtendShares(dataSquareBytes)
	if err != nil {
		logInvalidPropBlockError(app.Logger(), blockHeader, "failure to erasure the data square", err)
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

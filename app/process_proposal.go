package app

import (
	"bytes"
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/square"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

const rejectedPropBlockLog = "Rejected proposal block:"

func (app *App) ProcessProposal(req abci.RequestProcessProposal) abci.ResponseProcessProposal {
	// create the anteHanders that are used to check the validity of
	// transactions. We verify the signatures of PFB containing txs using the
	// sigVerifyAnterHandler, and simply increase the nonce of all other
	// transactions.
	svHander := sigVerifyAnteHandler(&app.AccountKeeper, app.txConfig)
	seqHandler := incrementSequenceAnteHandler(&app.AccountKeeper)
	sdkCtx, err := app.NewProcessProposalQueryContext()
	if err != nil {
		logInvalidPropBlockError(app.Logger(), req.Header, "failure to load query context", err)
		return reject()
	}

	// iterate over all txs and ensure that all blobTxs are valid, PFBs are correctly signed and non
	// blobTxs have no PFBs present
	for idx, rawTx := range req.BlockData.Txs {
		tx := rawTx
		blobTx, isBlobTx := coretypes.UnmarshalBlobTx(rawTx)
		if isBlobTx {
			tx = blobTx.Tx
		}

		sdkTx, err := app.txConfig.TxDecoder()(tx)
		if err != nil {
			// we don't reject the block here because it is not a block validity
			// rule that all transactions included in the block data are
			// decodable
			continue
		}

		// handle non-blob transactions first
		if !isBlobTx {
			_, has := hasPFB(sdkTx.GetMsgs())
			if has {
				// A non blob tx has a PFB, which is invalid
				logInvalidPropBlock(app.Logger(), req.Header, fmt.Sprintf("tx %d has PFB but is not a blob tx", idx))
				return reject()
			}

			// we need to increment the sequence for every transaction so that
			// the signature check below is accurate. this error only gets hit
			// if the account in question doens't exist.
			sdkCtx, err = seqHandler(sdkCtx, sdkTx, false)
			if err != nil {
				logInvalidPropBlockError(app.Logger(), req.Header, "failure to incrememnt sequence", err)
				return reject()
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
		if err := blobtypes.ValidateBlobTx(app.txConfig, blobTx); err != nil {
			logInvalidPropBlockError(app.Logger(), req.Header, fmt.Sprintf("invalid blob tx %d", idx), err)
			return reject()
		}

		// validated the PFB signature
		sdkCtx, err = svHander(sdkCtx, sdkTx, true)
		if err != nil {
			logInvalidPropBlockError(app.Logger(), req.Header, "invalid PFB signature", err)
			return reject()
		}

	}

	// Construct the data square from the block's transactions
	dataSquare, err := square.Construct(req.BlockData.Txs, app.GetBaseApp().AppVersion(), app.GovSquareSizeUpperBound(sdkCtx))
	if err != nil {
		logInvalidPropBlockError(app.Logger(), req.Header, "failure to compute data square from transactions:", err)
		return reject()
	}

	// Assert that the square size stated by the proposer is correct
	if uint64(dataSquare.Size()) != req.BlockData.SquareSize {
		logInvalidPropBlock(app.Logger(), req.Header, "proposed square size differs from calculated square size")
		return reject()
	}

	eds, err := da.ExtendShares(shares.ToBytes(dataSquare))
	if err != nil {
		logInvalidPropBlockError(app.Logger(), req.Header, "failure to erasure the data square", err)
		return reject()
	}

	dah := da.NewDataAvailabilityHeader(eds)
	// by comparing the hashes we know the computed IndexWrappers (with the share indexes of the PFB's blobs)
	// are identical and that square layout is consistent. This also means that the share commitment rules
	// have been followed and thus each blobs share commitment should be valid
	if !bytes.Equal(dah.Hash(), req.Header.DataHash) {
		logInvalidPropBlock(app.Logger(), req.Header, "proposed data root differs from calculated data root")
		return reject()
	}

	return accept()
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

func reject() abci.ResponseProcessProposal {
	return abci.ResponseProcessProposal{
		Result: abci.ResponseProcessProposal_REJECT,
	}
}

func accept() abci.ResponseProcessProposal {
	return abci.ResponseProcessProposal{
		Result: abci.ResponseProcessProposal_ACCEPT,
	}
}

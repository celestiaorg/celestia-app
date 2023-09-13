package app

import (
	"bytes"
	"fmt"

	"github.com/celestiaorg/celestia-app/app/ante"
	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/square"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cometbft/cometbft/crypto/tmhash"
	"github.com/cometbft/cometbft/libs/log"
	coretypes "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const rejectedPropBlockLog = "Rejected proposal block:"

func (app *App) HandleProcessProposal(ctx sdk.Context, req abci.RequestProcessProposal) (resp abci.ResponseProcessProposal) {
	// Create the anteHander that are used to check the validity of
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
	)

	if len(req.Txs) == 0 {
		logInvalidPropBlock(app.Logger(), req.ProposerAddress, "no data hash included")
		return reject()
	}
	dataHash := req.Txs[len(req.Txs)-1]
	if len(dataHash) != tmhash.Size {
		logInvalidPropBlock(app.Logger(), req.ProposerAddress, "invalid data hash length")
		return reject()
	}
	txs := req.Txs[:len(req.Txs)-1]

	// iterate over all txs and ensure that all blobTxs are valid, PFBs are correctly signed and non
	// blobTxs have no PFBs present
	for idx, rawTx := range txs {
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
				logInvalidPropBlock(app.Logger(), req.ProposerAddress, fmt.Sprintf("tx %d has PFB but is not a blob tx", idx))
				return reject()
			}

			// we need to increment the sequence for every transaction so that
			// the signature check below is accurate. this error only gets hit
			// if the account in question doens't exist.
			ctx, err = handler(ctx, sdkTx, false)
			if err != nil {
				logInvalidPropBlockError(app.Logger(), req.ProposerAddress, "failure to incrememnt sequence", err)
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
			logInvalidPropBlockError(app.Logger(), req.ProposerAddress, fmt.Sprintf("invalid blob tx %d", idx), err)
			return reject()
		}

		// validated the PFB signature
		ctx, err = handler(ctx, sdkTx, false)
		if err != nil {
			logInvalidPropBlockError(app.Logger(), req.ProposerAddress, "invalid PFB signature", err)
			return reject()
		}

	}

	// Construct the data square from the block's transactions
	dataSquare, err := square.Construct(req.Txs, app.GetBaseApp().AppVersion(), app.GovSquareSizeUpperBound(ctx))
	if err != nil {
		logInvalidPropBlockError(app.Logger(), req.ProposerAddress, "failure to compute data square from transactions:", err)
		return reject()
	}

	eds, err := da.ExtendShares(shares.ToBytes(dataSquare))
	if err != nil {
		logInvalidPropBlockError(app.Logger(), req.ProposerAddress, "failure to erasure the data square", err)
		return reject()
	}

	dah, err := da.NewDataAvailabilityHeader(eds)
	if err != nil {
		logInvalidPropBlockError(app.Logger(), req.ProposerAddress, "failure to create new data availability header", err)
		return reject()
	}
	// by comparing the hashes we know the computed IndexWrappers (with the share indexes of the PFB's blobs)
	// are identical and that square layout is consistent. This also means that the share commitment rules
	// have been followed and thus each blobs share commitment should be valid
	if !bytes.Equal(dah.Hash(), dataHash) {
		logInvalidPropBlock(app.Logger(), req.ProposerAddress, "proposed data root differs from calculated data root")
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

func logInvalidPropBlock(l log.Logger, proposer []byte, reason string) {
	l.Error(
		rejectedPropBlockLog,
		"reason",
		reason,
		"proposer",
		proposer,
	)
}

func logInvalidPropBlockError(l log.Logger, proposer []byte, reason string, err error) {
	l.Error(
		rejectedPropBlockLog,
		"reason",
		reason,
		"proposer",
		proposer,
		"err",
		err.Error(),
	)
}

func reject() abci.ResponseProcessProposal {
	return abci.ResponseProcessProposal{
		Status: abci.ResponseProcessProposal_REJECT,
	}
}

func accept() abci.ResponseProcessProposal {
	return abci.ResponseProcessProposal{
		Status: abci.ResponseProcessProposal_ACCEPT,
	}
}

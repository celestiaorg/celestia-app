package app

import (
	"bytes"
	"fmt"
	"sort"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/inclusion"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/celestiaorg/rsmt2d"
	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

const (
	rejectedPropBlockLog = "Rejected proposal block:"
)

func (app *App) ProcessProposal(req abci.RequestProcessProposal) abci.ResponseProcessProposal {
	// Check for blob inclusion:
	//  - each MsgPayForBlobs included in a block should have a corresponding blob data in the block body
	//  - the commitment in each PFB should match the commitment for the shares that contain that blob data
	//  - there should be no unpaid-for data

	data, err := coretypes.DataFromProto(req.BlockData)
	if err != nil {
		logInvalidPropBlockError(app.Logger(), req.Header, "failure to unmarshal block data:", err)
		return abci.ResponseProcessProposal{
			Result: abci.ResponseProcessProposal_REJECT,
		}
	}

	if !sort.IsSorted(coretypes.BlobsByNamespace(data.Blobs)) {
		logInvalidPropBlock(app.Logger(), req.Header, "blobs are unsorted")
		return abci.ResponseProcessProposal{
			Result: abci.ResponseProcessProposal_REJECT,
		}
	}

	for _, blob := range data.Blobs {
		if !isValidBlobNamespace(blob.NamespaceID) {
			logInvalidPropBlock(app.Logger(), req.Header, fmt.Sprintf("invalid blob namespace %v", blob.NamespaceID))
			return abci.ResponseProcessProposal{
				Result: abci.ResponseProcessProposal_REJECT,
			}
		}
	}

	if !arePFBsOrderedAfterTxs(req.BlockData.Txs) {
		logInvalidPropBlock(app.Logger(), req.Header, "PFBs are not all ordered at the end of the list of transactions")
		return abci.ResponseProcessProposal{
			Result: abci.ResponseProcessProposal_REJECT,
		}
	}

	dataSquare, err := shares.Split(data, true)
	if err != nil {
		logInvalidPropBlockError(app.Logger(), req.Header, "failure to compute shares from block data:", err)
		return abci.ResponseProcessProposal{
			Result: abci.ResponseProcessProposal_REJECT,
		}
	}

	cacher := inclusion.NewSubtreeCacher(data.SquareSize)
	eds, err := rsmt2d.ComputeExtendedDataSquare(shares.ToBytes(dataSquare), appconsts.DefaultCodec(), cacher.Constructor)
	if err != nil {
		logInvalidPropBlockError(app.Logger(), req.Header, "failure to erasure the data square", err)
		return abci.ResponseProcessProposal{
			Result: abci.ResponseProcessProposal_REJECT,
		}
	}

	dah := da.NewDataAvailabilityHeader(eds)

	if !bytes.Equal(dah.Hash(), req.Header.DataHash) {
		logInvalidPropBlock(app.Logger(), req.Header, "proposed data root differs from calculated data root")
		return abci.ResponseProcessProposal{
			Result: abci.ResponseProcessProposal_REJECT,
		}
	}

	// create the anteHanders that are used to check the validity of
	// transactions. We verify the signatures of PFB containing txs using the
	// sigVerifyAnterHandler, and simply increase the nonce of all other
	// transactions.
	svHander := sigVerifyAnteHandler(&app.AccountKeeper, app.txConfig)
	seqHandler := incrementSequenceAnteHandler(&app.AccountKeeper)

	sdkCtx, err := app.NewProcessProposalQueryContext()
	if err != nil {
		logInvalidPropBlockError(app.Logger(), req.Header, "failure to load query context", err)
		return abci.ResponseProcessProposal{
			Result: abci.ResponseProcessProposal_REJECT,
		}
	}

	// iterate over all of the MsgPayForBlob transactions and ensure that their
	// commitments are subtree roots of the data root.
	for _, rawTx := range req.BlockData.Txs {
		tx := rawTx
		wrappedTx, isWrapped := coretypes.UnmarshalIndexWrapper(rawTx)
		if isWrapped {
			tx = wrappedTx.Tx
		}

		sdkTx, err := app.txConfig.TxDecoder()(tx)
		if err != nil {
			// we don't reject the block here because it is not a block validity
			// rule that all transactions included in the block data are
			// decodable
			continue
		}

		pfb, has := hasPFB(sdkTx.GetMsgs())
		if !has {
			// we need to increment the sequence for every transaction so that
			// the signature check below is accurate. this error only gets hit
			// if the account in question doens't exist.
			sdkCtx, err = seqHandler(sdkCtx, sdkTx, false)
			if err != nil {
				logInvalidPropBlockError(app.Logger(), req.Header, "failure to incrememnt sequence", err)
				return abci.ResponseProcessProposal{
					Result: abci.ResponseProcessProposal_REJECT,
				}
			}

			// we do not need to perform further checks on this transaction,
			// since it has no PFB
			continue
		}

		// ensure there is only a single sdk.Msg included in the transaction
		if len(sdkTx.GetMsgs()) > 1 {
			logInvalidPropBlock(app.Logger(), req.Header, "invalid PFB found: combined with one or more other sdk.Msg")
			return abci.ResponseProcessProposal{
				Result: abci.ResponseProcessProposal_REJECT,
			}
		}

		// all PFBs must have a share index, so that we can find their
		// respective blob.
		if !isWrapped {
			logInvalidPropBlock(app.Logger(), req.Header, "Found a MsgPayForBlobs without a share index")
			return abci.ResponseProcessProposal{
				Result: abci.ResponseProcessProposal_REJECT,
			}
		}

		if err = pfb.ValidateBasic(); err != nil {
			logInvalidPropBlockError(app.Logger(), req.Header, "invalid MsgPayForBlobs", err)
			return abci.ResponseProcessProposal{
				Result: abci.ResponseProcessProposal_REJECT,
			}
		}

		for i, shareIndex := range wrappedTx.ShareIndexes {
			commitment, err := inclusion.GetCommit(cacher, dah, int(shareIndex), shares.SparseSharesNeeded(pfb.BlobSizes[i]))
			if err != nil {
				logInvalidPropBlockError(app.Logger(), req.Header, "commitment not found", err)
				return abci.ResponseProcessProposal{
					Result: abci.ResponseProcessProposal_REJECT,
				}
			}
			if !bytes.Equal(pfb.ShareCommitments[i], commitment) {
				logInvalidPropBlock(app.Logger(), req.Header, "found commitment does not match user's")
				return abci.ResponseProcessProposal{
					Result: abci.ResponseProcessProposal_REJECT,
				}
			}
		}

		sdkCtx, err = svHander(sdkCtx, sdkTx, true)
		if err != nil {
			logInvalidPropBlockError(app.Logger(), req.Header, "invalid PFB signature", err)
			return abci.ResponseProcessProposal{
				Result: abci.ResponseProcessProposal_REJECT,
			}
		}
	}

	return abci.ResponseProcessProposal{
		Result: abci.ResponseProcessProposal_ACCEPT,
	}
}

func hasPFB(msgs []sdk.Msg) (*blobtypes.MsgPayForBlobs, bool) {
	for _, msg := range msgs {
		if pfb, ok := msg.(*blobtypes.MsgPayForBlobs); ok {
			return pfb, true
		}
	}
	return nil, false
}

func arePFBsOrderedAfterTxs(txs [][]byte) bool {
	seenFirstPFB := false
	for _, tx := range txs {
		_, isWrapped := coretypes.UnmarshalIndexWrapper(tx)
		if isWrapped {
			seenFirstPFB = true
		} else if seenFirstPFB {
			return false
		}
	}
	return true
}

func isValidBlobNamespace(namespace namespace.ID) bool {
	isReserved := bytes.Compare(namespace, appconsts.MaxReservedNamespace) <= 0
	isParity := bytes.Equal(namespace, appconsts.ParitySharesNamespaceID)
	isTailPadding := bytes.Equal(namespace, appconsts.TailPaddingNamespaceID)
	return !isReserved && !isParity && !isTailPadding
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

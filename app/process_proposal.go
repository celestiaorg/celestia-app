package app

import (
	"bytes"
	"sort"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/inclusion"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/celestiaorg/rsmt2d"
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
	//  - each MsgPayForBlob included in a block should have a corresponding blob data in the block body
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

	// iterate over all of the MsgPayForBlob transactions and ensure that their
	// commitments are subtree roots of the data root.
	commitmentCounter := 0
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

		for _, msg := range sdkTx.GetMsgs() {
			pfb, ok := msg.(*types.MsgPayForBlob)
			if !ok {
				continue
			}

			// all PFBs must have a share index, so that we can find their
			// respective blob.
			if !isWrapped {
				logInvalidPropBlock(app.Logger(), req.Header, "Found a MsgPayForBlob without a share index")
				return abci.ResponseProcessProposal{
					Result: abci.ResponseProcessProposal_REJECT,
				}
			}

			if err = pfb.ValidateBasic(); err != nil {
				logInvalidPropBlockError(app.Logger(), req.Header, "invalid MsgPayForBlob", err)
				return abci.ResponseProcessProposal{
					Result: abci.ResponseProcessProposal_REJECT,
				}
			}

			commitment, err := inclusion.GetCommit(cacher, dah, int(wrappedTx.ShareIndex), shares.SparseSharesNeeded(uint32(pfb.BlobSize)))
			if err != nil {
				logInvalidPropBlockError(app.Logger(), req.Header, "commitment not found", err)
				return abci.ResponseProcessProposal{
					Result: abci.ResponseProcessProposal_REJECT,
				}
			}

			if !bytes.Equal(pfb.ShareCommitment, commitment) {
				logInvalidPropBlock(app.Logger(), req.Header, "found commitment does not match user's")
				return abci.ResponseProcessProposal{
					Result: abci.ResponseProcessProposal_REJECT,
				}
			}

			commitmentCounter++
		}
	}

	// compare the number of MPFBs and blobs, if they aren't
	// identical, then we already know this block is invalid
	if commitmentCounter != len(req.BlockData.Blobs) {
		logInvalidPropBlock(app.Logger(), req.Header, "varying number of MsgPayForBlob and blobs in the same block")
		return abci.ResponseProcessProposal{
			Result: abci.ResponseProcessProposal_REJECT,
		}
	}
	return abci.ResponseProcessProposal{
		Result: abci.ResponseProcessProposal_ACCEPT,
	}
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

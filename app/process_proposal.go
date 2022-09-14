package app

import (
	"bytes"

	"github.com/celestiaorg/celestia-app/pkg/da"
	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"

	"github.com/celestiaorg/celestia-app/app/encoding"
	shares "github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/x/payment/types"
)

const (
	rejectedPropBlockLog = "Rejected proposal block:"
)

func (app *App) ProcessProposal(req abci.RequestProcessProposal) abci.ResponseProcessProposal {
	// Check for message inclusion:
	//  - each MsgPayForData included in a block should have a corresponding data also in the block body
	//  - the commitment in each PFD should match that of its corresponding data
	//  - there should be no unpaid-for data

	// extract the commitments from any MsgPayForDatas in the block
	commitments := make(map[string]struct{})
	// we have a separate counter so that identical data also get counted
	// also see https://github.com/celestiaorg/celestia-app/issues/226
	commitmentCounter := 0
	for _, rawTx := range req.BlockData.Txs {
		tx, err := encoding.MalleatedTxDecoder(app.txConfig.TxDecoder())(rawTx)
		if err != nil {
			continue
		}

		for _, msg := range tx.GetMsgs() {
			if sdk.MsgTypeURL(msg) != types.URLMsgPayForData {
				continue
			}

			pfd, ok := msg.(*types.MsgPayForData)
			if !ok {
				app.Logger().Error("Msg type does not match MsgPayForData URL")
				continue
			}

			commitments[string(pfd.MessageShareCommitment)] = struct{}{}
			commitmentCounter++
		}
	}

	// quickly compare the number of PFDs and messages, if they aren't
	// identical, then  we already know this block is invalid
	if commitmentCounter != len(req.BlockData.Messages.MessagesList) {
		logInvalidPropBlock(app.Logger(), req.Header, "varying number of messages and payForData txs in the same block")
		return abci.ResponseProcessProposal{
			Result: abci.ResponseProcessProposal_REJECT,
		}
	}

	// iterate through all of the messages and ensure that a PFD with the exact
	// commitment exists
	for _, msg := range req.BlockData.Messages.MessagesList {
		if err := types.ValidateMessageNamespaceID(msg.NamespaceId); err != nil {
			logInvalidPropBlockError(app.Logger(), req.Header, "found a message that uses an invalid namespace id", err)
			return abci.ResponseProcessProposal{
				Result: abci.ResponseProcessProposal_REJECT,
			}
		}

		commit, err := types.CreateCommitment(req.BlockData.OriginalSquareSize, msg.NamespaceId, msg.Data)
		if err != nil {
			logInvalidPropBlockError(app.Logger(), req.Header, "failure to create commitment for included message", err)
			return abci.ResponseProcessProposal{
				Result: abci.ResponseProcessProposal_REJECT,
			}
		}

		// TODO: refactor to actually check for subtree roots instead of simply inclusion see issues #382 and #383
		if _, has := commitments[string(commit)]; !has {
			logInvalidPropBlock(app.Logger(), req.Header, "missing MsgPayForData for included message")
			return abci.ResponseProcessProposal{
				Result: abci.ResponseProcessProposal_REJECT,
			}
		}
	}

	data, err := coretypes.DataFromProto(req.BlockData)
	if err != nil {
		logInvalidPropBlockError(app.Logger(), req.Header, "failure to unmarshal block data:", err)
		return abci.ResponseProcessProposal{
			Result: abci.ResponseProcessProposal_REJECT,
		}
	}

	if !data.Messages.IsSorted() {
		logInvalidPropBlock(app.Logger(), req.Header, "messages are unsorted")
		return abci.ResponseProcessProposal{
			Result: abci.ResponseProcessProposal_REJECT,
		}
	}

	rawShares, err := shares.Split(data)
	if err != nil {
		logInvalidPropBlockError(app.Logger(), req.Header, "failure to compute shares from block data:", err)
		return abci.ResponseProcessProposal{
			Result: abci.ResponseProcessProposal_REJECT,
		}
	}

	eds, err := da.ExtendShares(req.BlockData.OriginalSquareSize, rawShares)
	if err != nil {
		logInvalidPropBlockError(app.Logger(), req.Header, "failure to erasure the data square", err)
		return abci.ResponseProcessProposal{
			Result: abci.ResponseProcessProposal_REJECT,
		}
	}

	dah := da.NewDataAvailabilityHeader(eds)

	if !bytes.Equal(dah.Hash(), req.Header.DataHash) {
		logInvalidPropBlockError(app.Logger(), req.Header, "proposed data root differs from calculated data root", err)
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

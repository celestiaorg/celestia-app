package app

import (
	"bytes"

	"github.com/celestiaorg/celestia-app/x/payment/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/pkg/da"
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
		tx, err := MalleatedTxDecoder(app.txConfig.TxDecoder())(rawTx)
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
		app.Logger().Error(
			rejectedPropBlockLog,
			"reason",
			"varying number of messages and payForData txs in the same block",
		)
		return abci.ResponseProcessProposal{
			Result: abci.ResponseProcessProposal_REJECT,
		}
	}

	// iterate through all of the messages and ensure that a PFD with the exact
	// commitment exists
	for _, msg := range req.BlockData.Messages.MessagesList {
		commit, err := types.CreateCommitment(req.BlockData.OriginalSquareSize, msg.NamespaceId, msg.Data)
		if err != nil {
			app.Logger().Error(
				rejectedPropBlockLog,
				"reason",
				"failure to create commitment for included message",
				"error",
				err.Error(),
			)
			return abci.ResponseProcessProposal{
				Result: abci.ResponseProcessProposal_REJECT,
			}
		}

		// TODO: refactor to actually check for subtree roots instead of simply inclusion see issues #382 and #383
		if _, has := commitments[string(commit)]; !has {
			app.Logger().Info(rejectedPropBlockLog, "reason", "missing MsgPayForData for included message")
			return abci.ResponseProcessProposal{
				Result: abci.ResponseProcessProposal_REJECT,
			}
		}
	}

	// check that the data matches the data hash
	dataSquare, _, err := WriteSquare(app.txConfig, req.BlockData.OriginalSquareSize, req.BlockData)
	if err != nil {
		// todo(evan): see if we can get rid of this panic
		panic(err)
	}

	eds, err := da.ExtendShares(req.BlockData.OriginalSquareSize, dataSquare)
	if err != nil {
		app.Logger().Error(
			rejectedPropBlockLog,
			"reason",
			"failure to erasure the data square",
			"error",
			err.Error(),
		)
		return abci.ResponseProcessProposal{
			Result: abci.ResponseProcessProposal_REJECT,
		}
	}

	dah := da.NewDataAvailabilityHeader(eds)

	if !bytes.Equal(dah.Hash(), req.Header.DataHash) {
		app.Logger().Info(
			rejectedPropBlockLog,
			"reason",
			"proposed data root differs from calculated data root",
		)
		return abci.ResponseProcessProposal{
			Result: abci.ResponseProcessProposal_REJECT,
		}
	}

	return abci.ResponseProcessProposal{
		Result: abci.ResponseProcessProposal_ACCEPT,
	}
}

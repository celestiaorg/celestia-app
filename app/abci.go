package app

import (
	"github.com/lazyledger/lazyledger-app/x/lazyledgerapp/types"
	abci "github.com/lazyledger/lazyledger-core/abci/types"
	core "github.com/lazyledger/lazyledger-core/proto/tendermint/types"
)

// This file should contain all of the althered ABCI methods

// PreprocessTxs fullfills the lazyledger-core version of the ACBI interface, by
// performing basic validation for the incoming txs, and by cleanly separating
// share messages from transactions
// todo(evan): refactor out a for loop.
func (app *App) PreprocessTxs(txs abci.RequestPreprocessTxs) abci.ResponsePreprocessTxs {
	var shareMsgs []*core.Message
	for _, rawTx := range txs.Txs {
		// decode the Tx
		tx, err := app.txDecoder(rawTx)
		if err != nil {
			continue
		}

		// run basic validation on the transaction
		err = tx.ValidateBasic()
		if err != nil {
			continue
		}

		// don't allow transactions with multiple messages to be ran
		if len(tx.GetMsgs()) != 1 {
			continue
		}

		msg := tx.GetMsgs()[0]

		// run basic validation on the msg
		err = msg.ValidateBasic()
		if err != nil {
			continue
		}

		// quickly check the stated type of the message
		if msg.Type() != types.TypeMsgPayforMessage {
			continue
		}

		// double check the actual type of the message
		wireMsg, ok := msg.(*types.MsgWirePayForMessage)
		if !ok {
			continue
		}

		// todo(evan): this needs to turn the transaction into a SignedTransactionDataPayForMessage
		// // discard the share commitments that won't be used
		// var shareCommit types.ShareCommitAndSignature
		// for _, commit := range wireMsg.MessageShareCommitment {
		// 	if commit.K == app.SquareSize() {
		// 		shareCommit = commit
		// 		break
		// 	}
		// }

		// make sure to get rid of excess txs or do something with them

		// execute the tx in runTxModeDeliver mode (3)
		// execution includes all validation checks burning fees
		_, _, err = app.BaseApp.TxRunner()(3, rawTx)
		if err != nil {
			continue
		}

		// the message is valid and paid for, include it in the response
		shareMsgs = append(
			shareMsgs,
			&core.Message{
				NamespaceId: wireMsg.GetMessageNameSpaceId(),
				Data:        wireMsg.Message,
			},
		)
	}

	return abci.ResponsePreprocessTxs{
		Txs:      txs.Txs,
		Messages: &core.Messages{MessagesList: shareMsgs},
	}
}

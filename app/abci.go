package app

import (
	"bytes"
	"errors"
	"fmt"
	"sort"

	"github.com/celestiaorg/celestia-app/x/payment/types"
	abci "github.com/celestiaorg/celestia-core/abci/types"
	core "github.com/celestiaorg/celestia-core/proto/tendermint/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// This file should contain all of the altered ABCI methods

// PreprocessTxs fullfills the celestia-core version of the ACBI interface, by
// performing basic validation for the incoming txs, and by cleanly separating
// share messages from transactions
func (app *App) PreprocessTxs(txs abci.RequestPreprocessTxs) abci.ResponsePreprocessTxs {
	squareSize := app.SquareSize()
	shareCounter := uint64(0)
	var shareMsgs []*core.Message
	var processedTxs [][]byte
	for _, rawTx := range txs.Txs {
		// decode the Tx
		tx, err := app.txConfig.TxDecoder()(rawTx)
		if err != nil {
			continue
		}

		// don't process the tx if the transaction doesn't contain a
		// PayForMessage sdk.Msg
		if !hasWirePayForMessage(tx) {
			processedTxs = append(processedTxs, rawTx)
			continue
		}

		// only support transactions that contain a single sdk.Msg
		if len(tx.GetMsgs()) != 1 {
			continue
		}

		msg := tx.GetMsgs()[0]

		// run basic validation on the transaction
		err = tx.ValidateBasic()
		if err != nil {
			continue
		}

		// process the message
		coreMsg, signedTx, err := app.processMsg(msg)
		if err != nil {
			continue
		}

		// increment the share counter by the number of shares taken by the message
		sharesTaken := uint64(len(coreMsg.Data) / types.ShareSize)
		shareCounter += sharesTaken

		// if there are too many shares stop processing and return the transactions
		if shareCounter > squareSize*squareSize {
			break
		}

		// encode the processed tx
		rawProcessedTx, err := app.appCodec.MarshalBinaryBare(signedTx)
		if err != nil {
			continue
		}

		// add the message and tx to the output
		shareMsgs = append(shareMsgs, &coreMsg)
		processedTxs = append(processedTxs, rawProcessedTx)
	}

	// sort messages lexigraphically
	sort.Slice(shareMsgs, func(i, j int) bool {
		return bytes.Compare(shareMsgs[i].NamespaceId, shareMsgs[j].NamespaceId) < 0
	})

	return abci.ResponsePreprocessTxs{
		Txs:      processedTxs,
		Messages: &core.Messages{MessagesList: shareMsgs},
	}
}

func hasWirePayForMessage(tx sdk.Tx) bool {
	for _, msg := range tx.GetMsgs() {
		if msg.Type() == types.TypeMsgPayforMessage {
			return true
		}
	}
	return false
}

// processMsgs will perform the processing required by PreProcessTxs for a set
// of sdk.Msg's from a single sdk.Tx
func (app *App) processMsg(msg sdk.Msg) (core.Message, *types.TxSignedTransactionDataPayForMessage, error) {
	squareSize := app.SquareSize()
	// reject all msgs in tx if a single included msg is not correct type
	wireMsg, ok := msg.(*types.MsgWirePayForMessage)
	if !ok {
		return core.Message{},
			nil,
			errors.New("transaction contained a message type other than types.MsgWirePayForMessage")
	}

	// make sure that a ShareCommitAndSignature of the correct size is
	// included in the message
	var shareCommit types.ShareCommitAndSignature
	for _, commit := range wireMsg.MessageShareCommitment {
		if commit.K == squareSize {
			shareCommit = commit
		}
	}
	// K == 0 means there was no share commit with the desired current square size
	if shareCommit.K == 0 {
		return core.Message{},
			nil,
			fmt.Errorf("No share commit for correct square size. Current square size: %d", squareSize)
	}

	// add the message to the list of core message to be returned to ll-core
	coreMsg := core.Message{
		NamespaceId: wireMsg.GetMessageNameSpaceId(),
		Data:        wireMsg.GetMessage(),
	}

	// wrap the signed transaction data
	sTxData, err := wireMsg.SignedTransactionDataPayForMessage(squareSize)
	if err != nil {
		return core.Message{}, nil, err
	}

	signedData := &types.TxSignedTransactionDataPayForMessage{
		Message:   sTxData,
		Signature: shareCommit.Signature,
		PublicKey: wireMsg.PublicKey,
	}

	return coreMsg, signedData, nil
}

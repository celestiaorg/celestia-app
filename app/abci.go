package app

import (
	"bytes"
	"crypto/sha256"
	"sort"

	"github.com/celestiaorg/celestia-app/x/payment/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/pkg/consts"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

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

		authTx, ok := tx.(signing.Tx)
		if !ok {
			continue
		}

		// don't process the tx if the transaction doesn't contain a
		//  MsgPayForMessage sdk.Msg
		if !hasWirePayForMessage(authTx) {
			processedTxs = append(processedTxs, rawTx)
			continue
		}

		// only support transactions that contain a single sdk.Msg
		if len(authTx.GetMsgs()) != 1 {
			continue
		}

		msg := authTx.GetMsgs()[0]
		wireMsg, ok := msg.(*types.MsgWirePayForMessage)
		if !ok {
			continue
		}

		// run basic validation on the transaction
		err = authTx.ValidateBasic()
		if err != nil {
			continue
		}

		// parse wire message and create a single message
		coreMsg, unsignedPFM, sig, err := types.ProcessWirePayForMessage(wireMsg, app.SquareSize())
		if err != nil {
			continue
		}

		// create the signed PayForMessage using the fees, gas limit, and sequence from
		// the original transaction, along with the appropriate signature.
		signedTx, err := types.BuildPayForMessageTxFromWireTx(authTx, app.txConfig.NewTxBuilder(), sig, unsignedPFM)
		if err != nil {
			app.Logger().Error("failure to create signed PayForMessage", err)
			continue
		}

		// increment the share counter by the number of shares taken by the message
		sharesTaken := uint64(len(coreMsg.Data) / types.ShareSize)
		shareCounter += sharesTaken

		// if there are too many shares stop processing and return the transactions
		if shareCounter > squareSize*squareSize {
			break
		}

		rawProcessedTx, err := app.txConfig.TxEncoder()(signedTx)
		if err != nil {
			continue
		}

		parentHash := sha256.Sum256(rawTx)
		wrappedTx, err := coretypes.WrapMalleatedTx(parentHash[:], rawProcessedTx)
		if err != nil {
			app.Logger().Error("failure to wrap child transaction with parent hash", "Error:", err)
		}

		shareMsgs = append(shareMsgs, coreMsg)
		processedTxs = append(processedTxs, wrappedTx)
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
		msgName := sdk.MsgTypeURL(msg)
		if msgName == types.URLMsgWirePayforMessage {
			return true
		}
	}
	return false
}

// SquareSize returns the current square size. Currently, the square size is
// hardcoded. todo(evan): don't hardcode the square size
func (app *App) SquareSize() uint64 {
	return consts.MaxSquareSize
}
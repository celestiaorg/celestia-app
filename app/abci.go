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
func (app *App) PrepareProposal(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal {
	squareSize := app.SquareSize()
	shareCounter := uint64(0)
	var shareMsgs []*core.Message
	var processedTxs [][]byte
	for _, rawTx := range req.BlockData.Txs {
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
		//  MsgPayForData sdk.Msg
		if !hasWirePayForData(authTx) {
			processedTxs = append(processedTxs, rawTx)
			continue
		}

		// only support transactions that contain a single sdk.Msg
		if len(authTx.GetMsgs()) != 1 {
			continue
		}

		msg := authTx.GetMsgs()[0]
		wireMsg, ok := msg.(*types.MsgWirePayForData)
		if !ok {
			continue
		}

		// run basic validation on the transaction
		err = authTx.ValidateBasic()
		if err != nil {
			continue
		}

		// parse wire message and create a single message
		coreMsg, unsignedPFM, sig, err := types.ProcessWirePayForData(wireMsg, app.SquareSize())
		if err != nil {
			continue
		}

		// create the signed PayForData using the fees, gas limit, and sequence from
		// the original transaction, along with the appropriate signature.
		signedTx, err := types.BuildPayForDataTxFromWireTx(authTx, app.txConfig.NewTxBuilder(), sig, unsignedPFM)
		if err != nil {
			app.Logger().Error("failure to create signed PayForData", err)
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

	return abci.ResponsePrepareProposal{
		BlockData: &core.Data{
			Txs:      processedTxs,
			Evidence: req.BlockData.Evidence,
			Messages: core.Messages{MessagesList: shareMsgs},
		},
	}
}

func hasWirePayForData(tx sdk.Tx) bool {
	for _, msg := range tx.GetMsgs() {
		msgName := sdk.MsgTypeURL(msg)
		if msgName == types.URLMsgWirePayForData {
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

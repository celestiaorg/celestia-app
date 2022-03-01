package app

import (
	"math"

	"github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/pkg/consts"
	"github.com/tendermint/tendermint/pkg/da"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
)

// PreprocessTxs fullfills the celestia-core version of the ACBI interface, by
// performing basic validation for the incoming txs, and by cleanly separating
// share messages from transactions
func (app *App) PrepareProposal(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal {
	squareSize := app.estimateSquareSize(req.BlockData)

	// todo(evan): create the DAH using the square
	dataSquare, data, err := WriteSquare(app.txConfig, squareSize, req.BlockData)
	if err != nil {
		// todo(evan): see if we can get rid of this panic
		panic(err)
	}

	eds, err := da.ExtendShares(squareSize, dataSquare)
	if err != nil {
		app.Logger().Error(
			"failure to erasure the data square while creating a proposal block",
			"error",
			err.Error(),
		)
		panic(err)
	}

	dah := da.NewDataAvailabilityHeader(eds)
	data.Hash = dah.Hash()
	data.OriginalSquareSize = squareSize

	return abci.ResponsePrepareProposal{
		BlockData: data,
	}
}

// estimateSquareSize returns an estimate of the needed square size to fit the
// provided block data. It assumes that every malleatable tx has a viable commit
// for whatever square size that we end up picking.
func (app *App) estimateSquareSize(data *core.Data) uint64 {
	txBytes := 0
	for _, tx := range data.Txs {
		txBytes += len(tx) + delimLen(uint64(len(tx)))
	}
	txShareEstimate := txBytes / consts.TxShareSize
	if txBytes > 0 {
		txShareEstimate++ // add one to round up
	}

	evdBytes := 0
	for _, evd := range data.Evidence.Evidence {
		evdBytes += evd.Size() + delimLen(uint64(evd.Size()))
	}
	evdShareEstimate := evdBytes / consts.TxShareSize
	if evdBytes > 0 {
		evdShareEstimate++ // add one to round up
	}

	isrBytes := 0
	for _, isr := range data.IntermediateStateRoots.RawRootsList {
		isrBytes += len(isr) + delimLen(uint64(len(isr)))
	}
	isrShareEstimate := isrBytes / consts.TxShareSize
	if isrBytes > 0 {
		isrShareEstimate++ // add one to round up
	}

	msgShareEstimate := estimateMsgShares(app.txConfig, data.Txs)

	totalShareEstimate := txShareEstimate + evdShareEstimate + isrShareEstimate + msgShareEstimate

	estimatedSize := types.NextPowerOf2(uint64(math.Sqrt(float64(totalShareEstimate))))

	if estimatedSize > consts.MaxSquareSize {
		return consts.MaxSquareSize
	}

	if estimatedSize < consts.MinSquareSize {
		estimatedSize = consts.MinSquareSize
	}

	return estimatedSize
}

func estimateMsgShares(txConf client.TxConfig, txs [][]byte) int {
	msgShares := uint64(0)
	for _, rawTx := range txs {
		// decode the Tx
		tx, err := txConf.TxDecoder()(rawTx)
		if err != nil {
			continue
		}

		authTx, ok := tx.(signing.Tx)
		if !ok {
			continue
		}

		// write the tx to the square if it normal
		if !hasWirePayForMessage(authTx) {
			continue
		}

		// only support malleated transactions that contain a single sdk.Msg
		if len(authTx.GetMsgs()) != 1 {
			continue
		}

		msg := authTx.GetMsgs()[0]
		wireMsg, ok := msg.(*types.MsgWirePayForMessage)
		if !ok {
			continue
		}

		msgShares += (wireMsg.MessageSize / consts.MsgShareSize) + 1 // plus one to round up

	}

	return int(msgShares)
}

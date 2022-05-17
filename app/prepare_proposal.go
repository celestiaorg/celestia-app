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

// PrepareProposal fullfills the celestia-core version of the ABCI interface by
// preparing the proposal block data. The square size is determined by first
// estimating it via the size of the passed block data. Then the included
// MsgWirePayForData messages are malleated into MsgPayForData messages by
// separating the message and transaction that pays for that message. Lastly,
// this method generates the data root for the proposal block and passes it the
// blockdata.
func (app *App) PrepareProposal(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal {
	squareSize := app.estimateSquareSize(req.BlockData)

	dataSquare, data := SplitShares(app.txConfig, squareSize, req.BlockData)

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

	msgShareEstimate := estimateMsgShares(app.txConfig, data.Txs)

	totalShareEstimate := txShareEstimate + evdShareEstimate + msgShareEstimate
	sr := math.Sqrt(float64(totalShareEstimate))
	estimatedSize := types.NextHighestPowerOf2(uint64(sr))
	switch {
	case estimatedSize > consts.MaxSquareSize:
		return consts.MaxSquareSize
	case estimatedSize < consts.MinSquareSize:
		return consts.MinSquareSize
	default:
		return estimatedSize
	}
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

		// skip txs that don't contain messages
		if !hasWirePayForData(authTx) {
			continue
		}

		// only support malleated transactions that contain a single sdk.Msg
		if len(authTx.GetMsgs()) != 1 {
			continue
		}

		msg := authTx.GetMsgs()[0]
		wireMsg, ok := msg.(*types.MsgWirePayForData)
		if !ok {
			continue
		}

		msgSize := wireMsg.MessageSize
		delimSize := delimLen(msgSize)

		msgShares += ((msgSize + uint64(delimSize)) / consts.MsgShareSize) + 1 // plus one to round up

	}
	return int(msgShares)
}

package app

import (
	"github.com/celestiaorg/celestia-app/pkg/shares"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/pkg/da"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// PrepareProposal fullfills the celestia-core version of the ABCI interface by
// preparing the proposal block data. The square size is determined by first
// estimating it via the size of the passed block data. Then the included
// MsgWirePayForData messages are malleated into MsgPayForData messages by
// separating the message and transaction that pays for that message. Lastly,
// this method generates the data root for the proposal block and passes it back
// to tendermint via the blockdata.
func (app *App) PrepareProposal(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal {
	// parse the txs, extracting any MsgWirePayForData and performing basic
	// validation for each transaction. Invalid txs are ignored. Original order
	// of the txs is maintained.
	parsedTxs := parseTxs(app.txConfig, req.BlockData.Txs)

	// estimate the square size. This estimation errors on the side of larger
	// squares but can only return values within the min and max square size.
	squareSize, totalSharesUsed := estimateSquareSize(parsedTxs, req.BlockData.Evidence)

	// the totalSharesUsed can be larger that the max number of shares if we
	// reach the max square size. In this case, we must prune the deprioritized
	// txs (and their messages if they're pfd txs).
	if totalSharesUsed > int(squareSize*squareSize) {
		parsedTxs = prune(app.txConfig, parsedTxs, totalSharesUsed, int(squareSize))
	}

	// in this step we are processing any MsgWirePayForData transactions into
	// MsgPayForData and their repsective messages. The malleatedTxs contain the
	// the new sdk.Msg with the original tx's metadata (sequence number, gas
	// price etc).
	processedTxs, messages, err := malleateTxs(app.txConfig, squareSize, parsedTxs, req.BlockData.Evidence)
	if err != nil {
		panic(err)
	}

	blockData := core.Data{
		Txs:                processedTxs,
		Evidence:           req.BlockData.Evidence,
		Messages:           core.Messages{MessagesList: messages},
		OriginalSquareSize: squareSize,
	}

	coreData, err := coretypes.DataFromProto(&blockData)
	if err != nil {
		panic(err)
	}

	dataSquare, err := shares.Split(coreData)
	if err != nil {
		panic(err)
	}

	// erasure the data square which we use to create the data root.
	eds, err := da.ExtendShares(squareSize, dataSquare)
	if err != nil {
		app.Logger().Error(
			"failure to erasure the data square while creating a proposal block",
			"error",
			err.Error(),
		)
		panic(err)
	}

	// create the new data root by creating the data availability header (merkle
	// roots of each row and col of the erasure data).
	dah := da.NewDataAvailabilityHeader(eds)

	// We use the block data struct to pass the square size and calculated data
	// root to tendermint.
	blockData.Hash = dah.Hash()
	blockData.OriginalSquareSize = squareSize

	// tendermint doesn't need to use any of the erasure data, as only the
	// protobuf encoded version of the block data is gossiped.
	return abci.ResponsePrepareProposal{
		BlockData: &blockData,
	}
}

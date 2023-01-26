package app

import (
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/square"
	abci "github.com/tendermint/tendermint/abci/types"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
)

// PrepareProposal fulfills the celestia-core version of the ABCI interface by
// preparing the proposal block data. The square size is determined by first
// estimating it via the size of the passed block data. Then, this method
// generates the data root for the proposal block and passes it back to
// tendermint via the BlockData.
func (app *App) PrepareProposal(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal {
	// cap the amount of transactions in the block. See https://github.com/celestiaorg/celestia-app/issues/1209
	// TODO: find a better long term solution
	if len(req.BlockData.Txs) > appconsts.TransactionsPerBlockLimit {
		req.BlockData.Txs = req.BlockData.Txs[:appconsts.TransactionsPerBlockLimit]
	}

	square, txs := square.Construct(req.BlockData.Txs, appconsts.DefaultMaxSquareSize)
	squareSize := square.Size()

	blockData := core.Data{Txs: txs}

	// erasure the data square which we use to create the data root.
	eds, err := da.ExtendShares(squareSize, shares.ToBytes(square))
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

	// tendermint doesn't need to use any of the erasure data, as only the
	// protobuf encoded version of the block data is gossiped.
	return abci.ResponsePrepareProposal{
		BlockData: &blockData,
	}
}

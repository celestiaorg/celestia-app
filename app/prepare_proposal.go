package app

import (
	"bytes"
	"sort"

	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	abci "github.com/tendermint/tendermint/abci/types"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// PrepareProposal fullfills the celestia-core version of the ABCI interface by
// preparing the proposal block data. The square size is determined by first
// estimating it via the size of the passed block data. Then the included
// MsgWirePayForBlob is malleated into MsgPayForBlob by separating the blob from
// the wire message. Lastly, this method generates the data root for the
// proposal block and passes it back to tendermint via the BlockData.
func (app *App) PrepareProposal(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal {
	// parse the txs, extracting any valid MsgWirePayForBlob. Original order of
	// the txs is maintained.
	parsedTxs := parseTxs(app.txConfig, req.BlockData.Txs)

	// estimate the square size. This estimation errors on the side of larger
	// squares but can only return values within the min and max square size.
	squareSize, nonreservedStart := estimateSquareSize(parsedTxs)

	parsedTxs = addShareIndexes(squareSize, nonreservedStart, parsedTxs)

	// in this step we are processing any MsgWirePayForBlob transactions into
	// MsgPayForBlob and their respective blobPointers. The malleatedTxs contain the
	// the new sdk.Msg with the original tx's metadata (sequence number, gas
	// price etc).
	processedTxs, blobs := processTxs(app.Logger(), parsedTxs)

	// blobs must be sorted by namespace in order to create nmts, and therefore
	// are required for valid blocks
	sort.SliceStable(blobs, func(i, j int) bool {
		return bytes.Compare(blobs[i].NamespaceId, blobs[j].NamespaceId) < 0
	})

	blockData := core.Data{
		Txs:        processedTxs,
		Blobs:      blobs,
		SquareSize: squareSize,
	}

	coreData, err := coretypes.DataFromProto(&blockData)
	if err != nil {
		panic(err)
	}

	dataSquare, err := shares.Split(coreData, true)
	if err != nil {
		panic(err)
	}

	// erasure the data square which we use to create the data root.
	eds, err := da.ExtendShares(squareSize, shares.ToBytes(dataSquare))
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
	blockData.SquareSize = squareSize

	// tendermint doesn't need to use any of the erasure data, as only the
	// protobuf encoded version of the block data is gossiped.
	return abci.ResponsePrepareProposal{
		BlockData: &blockData,
	}
}

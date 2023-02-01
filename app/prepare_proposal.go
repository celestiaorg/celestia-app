package app

import (
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	abci "github.com/tendermint/tendermint/abci/types"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
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

	// parse the txs, extracting any valid BlobTxs. Original order of
	// the txs is maintained.
	normalTxs, blobTxs := separateTxs(app.txConfig, req.BlockData.Txs)

	sdkCtx, err := app.NewProcessProposalQueryContext()
	if err != nil {
		panic(err)
	}

	// increment the sequences of the standard cosmos-sdk transactions. Panics
	// from the anteHandler are caught and logged.
	seqHandler := incrementSequenceAnteHandler(&app.AccountKeeper)
	normalTxs, sdkCtx = filterStdTxs(app.Logger(), app.txConfig.TxDecoder(), sdkCtx, seqHandler, normalTxs)

	// check the signatures and increment the sequences of the blob txs,
	// and filter out any that fail. Panics from the anteHandler are caught and
	// logged.
	svHandler := sigVerifyAnteHandler(&app.AccountKeeper, app.txConfig)
	blobTxs, _ = filterBlobTxs(app.Logger(), app.txConfig.TxDecoder(), sdkCtx, svHandler, blobTxs)

	// estimate the square size. This estimation errs on the side of larger
	// squares but can only return values within the min and max square size.
	squareSize, nonreservedStart := estimateSquareSize(normalTxs, blobTxs)

	// finalizeLayout wraps any blob transactions with their final share index.
	// This requires sorting the blobs by namespace and potentially pruning
	// MsgPayForBlobs transactions and their respective blobs from the block if
	// they do not fit into the square.
	wrappedPFBTxs, blobs := finalizeBlobLayout(squareSize, nonreservedStart, blobTxs)

	blockData := core.Data{
		Txs:        append(normalTxs, wrappedPFBTxs...),
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
	// Note: uses the nmt wrapper to construct the tree.
	// checkout pkg/wrapper/nmt_wrapper.go for more information.
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

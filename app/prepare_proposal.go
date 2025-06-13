package app

import (
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/go-square/v2/share"

	"github.com/celestiaorg/celestia-app/v4/app/ante"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/pkg/da"
)

// PrepareProposalHandler fulfills the celestia-core version of the ABCI interface by
// preparing the proposal block data. This method generates the data root for
// the proposal block and passes it back to tendermint via the BlockData. Panics
// indicate a developer error and should immediately halt the node for
// visibility and so they can be quickly resolved.
func (app *App) PrepareProposalHandler(ctx sdk.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	defer telemetry.MeasureSince(time.Now(), "prepare_proposal")
	// Create a context using a branch of the state.
	handler := ante.NewAnteHandler(
		app.AccountKeeper,
		app.BankKeeper,
		app.BlobKeeper,
		app.FeeGrantKeeper,
		app.GetTxConfig().SignModeHandler(),
		ante.DefaultSigVerificationGasConsumer,
		app.IBCKeeper,
		app.MinFeeKeeper,
		&app.CircuitKeeper,
		app.GovParamFilters(),
	)

	fsb, err := NewFilteredSquareBuilder(
		handler,
		app.encodingConfig.TxConfig,
		app.MaxEffectiveSquareSize(ctx),
		appconsts.SubtreeRootThreshold,
	)
	if err != nil {
		panic(err)
	}

	txs := fsb.Fill(ctx, req.Txs)

	// Build the square from the set of valid and prioritised transactions.
	dataSquare, err := fsb.Build()
	if err != nil {
		panic(err)
	}

	// Erasure encode the data square to create the extended data square (eds).
	// Note: uses the nmt wrapper to construct the tree. See
	// pkg/wrapper/nmt_wrapper.go for more information.
	eds, err := da.ExtendShares(share.ToBytes(dataSquare))
	if err != nil {
		app.Logger().Error("failure to erasure the data square while creating a proposal block", "error", err.Error())
		panic(err)
	}

	dah, err := da.NewDataAvailabilityHeader(eds)
	if err != nil {
		app.Logger().Error("failure to create new data availability header", "error", err.Error())
		panic(err)
	}

	// Tendermint doesn't need to use any of the erasure data because only the
	// protobuf encoded version of the block data is gossiped. Therefore, the
	// eds is not returned here.
	return &abci.ResponsePrepareProposal{
		Txs:          txs,
		SquareSize:   uint64(dataSquare.Size()),
		DataRootHash: dah.Hash(), // also known as the data root
	}, nil
}

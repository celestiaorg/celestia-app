package app

import (
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app/ante"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/pkg/da"
	shares "github.com/celestiaorg/go-square/shares"
	square "github.com/celestiaorg/go-square/square"
	squarev2 "github.com/celestiaorg/go-square/v2"
	sharev2 "github.com/celestiaorg/go-square/v2/share"
	abci "github.com/cometbft/cometbft/abci/types"
	core "github.com/cometbft/cometbft/proto/tendermint/types"
	version "github.com/cometbft/cometbft/proto/tendermint/version"
	"github.com/cosmos/cosmos-sdk/telemetry"
)

// PrepareProposal fulfills the celestia-core version of the ABCI interface by
// preparing the proposal block data. This method generates the data root for
// the proposal block and passes it back to tendermint via the BlockData. Panics
// indicate a developer error and should immediately halt the node for
// visibility and so they can be quickly resolved.
func (app *App) PrepareProposal(req abci.RequestPrepareProposal) abci.ResponsePrepareProposal {
	defer telemetry.MeasureSince(time.Now(), "prepare_proposal")
	// Create a context using a branch of the state.
	sdkCtx := app.NewProposalContext(core.Header{
		ChainID: req.ChainId,
		Height:  req.Height,
		Time:    req.Time,
		Version: version.Consensus{
			App: app.AppVersion(),
		},
	})
	handler := ante.NewAnteHandler(
		app.AccountKeeper,
		app.BankKeeper,
		app.BlobKeeper,
		app.FeeGrantKeeper,
		app.GetTxConfig().SignModeHandler(),
		ante.DefaultSigVerificationGasConsumer,
		app.IBCKeeper,
		app.ParamsKeeper,
		app.MsgGateKeeper,
	)

	// Filter out invalid transactions.
	txs := FilterTxs(app.Logger(), sdkCtx, handler, app.txConfig, req.BlockData.Txs)

	// Build the square from the set of valid and prioritised transactions.
	// The txs returned are the ones used in the square and block.
	var (
		dataSquareBytes [][]byte
		err             error
		size            uint64
	)
	switch app.AppVersion() {
	case v3:
		var dataSquare squarev2.Square
		dataSquare, txs, err = squarev2.Build(txs,
			app.MaxEffectiveSquareSize(sdkCtx),
			appconsts.SubtreeRootThreshold(app.GetBaseApp().AppVersion()),
		)
		dataSquareBytes = sharev2.ToBytes(dataSquare)
		size = uint64(dataSquare.Size())
	case v2, v1:
		var dataSquare square.Square
		dataSquare, txs, err = square.Build(txs,
			app.MaxEffectiveSquareSize(sdkCtx),
			appconsts.SubtreeRootThreshold(app.GetBaseApp().AppVersion()),
		)
		dataSquareBytes = shares.ToBytes(dataSquare)
		size = uint64(dataSquare.Size())
	default:
		err = fmt.Errorf("unsupported app version: %d", app.AppVersion())
	}
	if err != nil {
		panic(err)
	}

	// Erasure encode the data square to create the extended data square (eds).
	// Note: uses the nmt wrapper to construct the tree. See
	// pkg/wrapper/nmt_wrapper.go for more information.
	eds, err := da.ExtendShares(dataSquareBytes)
	if err != nil {
		app.Logger().Error(
			"failure to erasure the data square while creating a proposal block",
			"error",
			err.Error(),
		)
		panic(err)
	}

	dah, err := da.NewDataAvailabilityHeader(eds)
	if err != nil {
		app.Logger().Error(
			"failure to create new data availability header",
			"error",
			err.Error(),
		)
		panic(err)
	}

	// Tendermint doesn't need to use any of the erasure data because only the
	// protobuf encoded version of the block data is gossiped. Therefore, the
	// eds is not returned here.
	return abci.ResponsePrepareProposal{
		BlockData: &core.Data{
			Txs:        txs,
			SquareSize: size,
			Hash:       dah.Hash(), // also known as the data root
		},
	}
}

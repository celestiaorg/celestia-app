package malicious

import (
	abci "github.com/cometbft/cometbft/abci/types"
	core "github.com/cometbft/cometbft/proto/tendermint/types"
	version "github.com/cometbft/cometbft/proto/tendermint/version"

	"github.com/celestiaorg/go-square/v2/share"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/ante"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/pkg/da"
)

// OutOfOrderPrepareProposal fulfills the celestia-core version of the ABCI
// interface by preparing the proposal block data. This version of the method is
// used to create malicious block proposals that fraud proofs can be created
// for. It will swap the order of two blobs in the square and then use the
// modified nmt to create a commitment over the modified square.
func (a *App) OutOfOrderPrepareProposal(req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	// create a context using a branch of the state and loaded using the
	// proposal height and chain-id
	sdkCtx := a.NewProposalContext(core.Header{
		ChainID: a.ChainID(),
		Height:  req.Height,
		Time:    req.Time,
		Version: version.Consensus{
			App: appconsts.LatestVersion,
		},
	})
	// filter out invalid transactions.
	// TODO: we can remove all state independent checks from the ante handler here such as signature verification
	// and only check the state dependent checks like fees and nonces as all these transactions have already
	// passed CheckTx.
	handler := ante.NewAnteHandler(
		a.AccountKeeper,
		a.BankKeeper,
		a.BlobKeeper,
		a.FeeGrantKeeper,
		a.GetTxConfig().SignModeHandler(),
		ante.DefaultSigVerificationGasConsumer,
		a.IBCKeeper,
		a.MinFeeKeeper,
		&a.CircuitKeeper,
		a.GovParamFilters(),
	)

	txs := app.FilterTxs(a.Logger(), sdkCtx, handler, a.GetTxConfig(), req.Txs)

	// build the square from the set of valid and prioritised transactions.
	// The txs returned are the ones used in the square and block
	dataSquare, txs, err := Build(txs, appconsts.LatestVersion, a.MaxEffectiveSquareSize(sdkCtx), OutOfOrderExport)
	if err != nil {
		panic(err)
	}

	// erasure the data square which we use to create the data root. Note: this
	// is using a modified version of nmt where the order of the namespaces is
	// not enforced.
	eds, err := ExtendShares(share.ToBytes(dataSquare))
	if err != nil {
		a.Logger().Error(
			"failure to erasure the data square while creating a proposal block",
			"error",
			err.Error(),
		)
		panic(err)
	}

	// create the new data root by creating the data availability header (merkle
	// roots of each row and col of the erasure data).
	dah, err := da.NewDataAvailabilityHeader(eds)
	if err != nil {
		panic(err)
	}

	// tendermint doesn't need to use any of the erasure data, as only the
	// protobuf encoded version of the block data is gossiped.
	return &abci.ResponsePrepareProposal{
		Txs:          txs,
		SquareSize:   uint64(dataSquare.Size()),
		DataRootHash: dah.Hash(),
	}, nil
}

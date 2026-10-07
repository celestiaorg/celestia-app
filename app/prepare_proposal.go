package app

import (
	"bytes"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/da"
	fibreante "github.com/celestiaorg/celestia-app/v10/x/fibre/ante"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/telemetry"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// PrepareProposalHandler fulfills the celestia-core version of the ABCI interface by
// preparing the proposal block data. This method generates the data root for
// the proposal block and passes it back to tendermint via the BlockData. Errors
// are returned instead of panicking to improve error handling and reduce attack surface.
func (app *App) PrepareProposalHandler(ctx sdk.Context, req *abci.RequestPrepareProposal) (*abci.ResponsePrepareProposal, error) {
	defer telemetry.MeasureSince(time.Now(), "prepare_proposal")
	// Create a context using a branch of the state.
	handler := app.newAnteHandler(app.GetTxConfig().SignModeHandler())
	maxSquareSize := app.MaxEffectiveSquareSize(ctx)

	fsb, err := NewFilteredSquareBuilder(
		handler,
		app.MsgServiceRouter(),
		app.encodingConfig.TxConfig,
		app.IBCKeeper.ChannelKeeper,
		maxSquareSize,
		appconsts.SubtreeRootThreshold,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create FilteredSquareBuilder: %w", err)
	}

	// Run the fibre BeginBlocker on the proposal branch, mirroring FinalizeBlock,
	// which pays out matured withdrawals and advances the freshness floor before
	// any tx. Pay-for-fibre settlement in Fill must see that escrow state. The
	// branch is discarded, so nothing commits.
	if err := app.FibreKeeper.BeginBlocker(ctx); err != nil {
		return nil, fmt.Errorf("failed to run fibre begin blocker on proposal branch: %w", err)
	}
	// Warm certificate and transaction-signature caches on independent state
	// branches. Fill still performs all stateful checks in transaction order.
	decodedPFF, waitPFF, finishPFF := app.startPFFPreverification(ctx, req.Txs)
	defer finishPFF()
	ctx = fibreante.WithVerifiedPFFSlot(ctx)
	var squareDone chan preparedSquareResult
	if len(req.Txs) > 0 && len(req.Txs) <= appconsts.MaxPayForFibreMessages && len(decodedPFF) == len(req.Txs) {
		messages := make([]*fibretypes.MsgPayForFibre, len(req.Txs))
		candidate := true
		var bytesUsed int64
		for i, rawTx := range req.Txs {
			if len(rawTx) > appconsts.MaxTxSize || bytesUsed+int64(len(rawTx))+10 > 32<<20 {
				candidate = false
				break
			}
			bytesUsed += int64(len(rawTx)) + 10
			if req.MaxTxBytes > 0 && bytesUsed > req.MaxTxBytes {
				candidate = false
				break
			}
			if decodedPFF[i] == nil {
				candidate = false
				break
			}
			messages[i], candidate = payForFibreMsg(decodedPFF[i])
			if !candidate {
				break
			}
		}
		if candidate {
			squareDone = make(chan preparedSquareResult, 1)
			go func() {
				squareDone <- app.computePreparedSquare(req.Txs, maxSquareSize, messages)
			}()
		}
	}
	if squareDone != nil {
		if write, ok := app.preparePFFFast(ctx, req.Txs, decodedPFF, handler, waitPFF); ok {
			finishPFF()
			candidate := <-squareDone
			squareDone = nil
			if candidate.err == nil {
				write()
				return &abci.ResponsePrepareProposal{
					Txs:          req.Txs,
					SquareSize:   candidate.size,
					DataRootHash: candidate.root,
				}, nil
			}
		}
	}

	finishPFF()
	txs := fsb.Fill(ctx, req.Txs, req.MaxTxBytes)
	if squareDone != nil {
		candidate := <-squareDone
		if candidate.err == nil && sameTransactions(txs, req.Txs) {
			return &abci.ResponsePrepareProposal{
				Txs:          txs,
				SquareSize:   candidate.size,
				DataRootHash: candidate.root,
			}, nil
		}
	}

	// Build the square from the set of valid and prioritised transactions.
	dataSquare, err := fsb.Build()
	if err != nil {
		return nil, fmt.Errorf("failed to build data square: %w", err)
	}

	// Erasure encode the data square to create the extended data square (eds).
	// Note: uses the nmt wrapper to construct the tree. See
	// pkg/wrapper/nmt_wrapper.go for more information.
	eds, err := da.ExtendSharesWithTreePool(share.ToBytes(dataSquare), app.TreePool())
	if err != nil {
		app.Logger().Error("failure to erasure the data square while creating a proposal block", "error", err.Error())
		return nil, fmt.Errorf("failure to erasure the data square while creating a proposal block: %w", err)
	}

	dah, err := da.NewDataAvailabilityHeader(eds)
	if err != nil {
		app.Logger().Error("failure to create new data availability header", "error", err.Error())
		return nil, fmt.Errorf("failure to create new data availability header: %w", err)
	}

	squareSize, err := dataSquare.Size()
	if err != nil {
		return nil, fmt.Errorf("failure to get data square size: %w", err)
	}

	// Tendermint doesn't need to use any of the erasure data because only the
	// protobuf encoded version of the block data is gossiped. Therefore, the
	// eds is not returned here.
	return &abci.ResponsePrepareProposal{
		Txs:          txs,
		SquareSize:   uint64(squareSize),
		DataRootHash: dah.Hash(), // also known as the data root
	}, nil
}

func sameTransactions(a, b [][]byte) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if !bytes.Equal(a[i], b[i]) {
			return false
		}
	}
	return true
}

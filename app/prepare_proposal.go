package app

import (
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/v7/app/ante"
	"github.com/celestiaorg/celestia-app/v7/app/params"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v7/pkg/da"
	feeaddresstypes "github.com/celestiaorg/celestia-app/v7/x/feeaddress/types"
	"github.com/celestiaorg/go-square/v3/share"
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
		return nil, fmt.Errorf("failed to create FilteredSquareBuilder: %w", err)
	}

	// Check fee address balance and inject fee forward tx if non-zero.
	// This converts fee address funds into real transaction fees for dashboard tracking.
	txsToProcess := req.Txs
	feeBalance := app.BankKeeper.GetBalance(ctx, feeaddresstypes.FeeAddress, params.BondDenom)
	if !feeBalance.IsZero() {
		feeForwardTx, err := app.createFeeForwardTx(ctx, feeBalance)
		if err != nil {
			app.Logger().Error("failed to create fee forward tx", "error", err.Error())
			// Continue without the fee forward tx - don't fail block production
		} else {
			// Prepend fee forward tx to the transactions list
			txsToProcess = append([][]byte{feeForwardTx}, req.Txs...)
		}
	}

	txs := fsb.Fill(ctx, txsToProcess)

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

	// Tendermint doesn't need to use any of the erasure data because only the
	// protobuf encoded version of the block data is gossiped. Therefore, the
	// eds is not returned here.
	return &abci.ResponsePrepareProposal{
		Txs:          txs,
		SquareSize:   uint64(dataSquare.Size()),
		DataRootHash: dah.Hash(), // also known as the data root
	}, nil
}

// createFeeForwardTx creates an unsigned MsgForwardFees transaction with the
// specified fee amount. The transaction has no signers - it's validated by
// ProcessProposal checking that tx fee == fee address balance.
func (app *App) createFeeForwardTx(_ sdk.Context, feeAmount sdk.Coin) ([]byte, error) {
	msg := feeaddresstypes.NewMsgForwardFees()

	// Build the transaction
	txBuilder := app.encodingConfig.TxConfig.NewTxBuilder()
	if err := txBuilder.SetMsgs(msg); err != nil {
		return nil, fmt.Errorf("failed to set message: %w", err)
	}

	// Set the fee to the fee address balance (this is the key part - makes it a real tx fee)
	txBuilder.SetFeeAmount(sdk.NewCoins(feeAmount))
	txBuilder.SetGasLimit(50000) // Minimal gas - the message handler does no computation

	// Encode the transaction (no signature needed)
	txBytes, err := app.encodingConfig.TxConfig.TxEncoder()(txBuilder.GetTx())
	if err != nil {
		return nil, fmt.Errorf("failed to encode tx: %w", err)
	}

	return txBytes, nil
}

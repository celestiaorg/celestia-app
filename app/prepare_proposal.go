package app

import (
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/v7/app/ante"
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

	// Inject fee forward tx if fee address has balance
	txsToProcess, feeBalance, err := app.injectFeeForwardTx(ctx, req.Txs)
	if err != nil {
		return nil, err
	}

	txs := fsb.Fill(ctx, txsToProcess)

	// Verify fee forward tx survived filtering (if we injected one)
	if err := app.verifyFeeForwardTxSurvived(feeBalance, txs); err != nil {
		return nil, err
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

	// Tendermint doesn't need to use any of the erasure data because only the
	// protobuf encoded version of the block data is gossiped. Therefore, the
	// eds is not returned here.
	return &abci.ResponsePrepareProposal{
		Txs:          txs,
		SquareSize:   uint64(dataSquare.Size()),
		DataRootHash: dah.Hash(), // also known as the data root
	}, nil
}

// injectFeeForwardTx checks the fee address balance and prepends a fee forward tx
// if non-zero. This converts fee address funds into real transaction fees for
// dashboard tracking.
//
// Consensus safety: ProcessProposal validates that the fee forward tx fee amount equals
// the fee address balance at the start of the block. Both PrepareProposal and ProcessProposal
// read from the same state snapshot, ensuring all validators agree on validity. The fee
// forward tx is prepended so it executes first in DeliverTx, draining the fee address
// before any subsequent transactions can add to it.
//
// Returns the txs to process (with fee forward tx prepended if applicable), the fee
// balance (for verification), and any error.
func (app *App) injectFeeForwardTx(ctx sdk.Context, txs [][]byte) ([][]byte, sdk.Coin, error) {
	feeBalance := app.BankKeeper.GetBalance(ctx, feeaddresstypes.FeeAddress, appconsts.BondDenom)
	if feeBalance.IsZero() {
		return txs, feeBalance, nil
	}

	feeForwardTx, err := app.createFeeForwardTx(feeBalance)
	if err != nil {
		// Fail explicitly rather than producing a block that ProcessProposal will reject.
		return nil, feeBalance, fmt.Errorf("failed to create fee forward tx: %w; fee_balance=%s", err, feeBalance.String())
	}

	// Prepend fee forward tx so it executes first
	return append([][]byte{feeForwardTx}, txs...), feeBalance, nil
}

// verifyFeeForwardTxSurvived performs defense-in-depth verification that the fee
// forward tx was not filtered out. If the fee address had a balance and we created
// a fee forward tx, it MUST be the first tx in the output. The fee forward tx should
// never be filtered since it has no signature requirements and uses minimal gas.
// If it's filtered, there's a bug in the ante handler chain.
//
// If feeBalance is zero, this is a no-op (no fee forward tx was injected).
func (app *App) verifyFeeForwardTxSurvived(feeBalance sdk.Coin, filteredTxs [][]byte) error {
	if feeBalance.IsZero() {
		return nil
	}

	if len(filteredTxs) == 0 {
		return fmt.Errorf("fee forward tx was filtered out (no txs remain); fee_balance=%s", feeBalance.String())
	}

	_, firstTxIsFeeForward, err := app.parseFeeForwardTx(filteredTxs[0])
	if err != nil {
		return fmt.Errorf("failed to parse fee forward tx: %w; fee_balance=%s", err, feeBalance.String())
	}
	if !firstTxIsFeeForward {
		return fmt.Errorf("fee forward tx was filtered out unexpectedly; fee_balance=%s", feeBalance.String())
	}

	return nil
}

// createFeeForwardTx creates an unsigned MsgForwardFees transaction with the
// specified fee amount. The transaction has no signers - it's validated by
// ProcessProposal checking that tx fee == fee address balance.
func (app *App) createFeeForwardTx(feeAmount sdk.Coin) ([]byte, error) {
	msg := feeaddresstypes.NewMsgForwardFees()

	// Build the transaction
	txBuilder := app.encodingConfig.TxConfig.NewTxBuilder()
	if err := txBuilder.SetMsgs(msg); err != nil {
		return nil, fmt.Errorf("failed to set message: %w", err)
	}

	// Set the fee to the fee address balance (this is the key part - makes it a real tx fee)
	txBuilder.SetFeeAmount(sdk.NewCoins(feeAmount))
	txBuilder.SetGasLimit(feeaddresstypes.FeeForwardGasLimit)

	// Encode the transaction (no signature needed)
	txBytes, err := app.encodingConfig.TxConfig.TxEncoder()(txBuilder.GetTx())
	if err != nil {
		return nil, fmt.Errorf("failed to encode tx: %w", err)
	}

	return txBytes, nil
}

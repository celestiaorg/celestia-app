package keeper

import (
	"context"
	"runtime"
	"sync"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/fibre/validator"
	fibreante "github.com/celestiaorg/celestia-app/v10/x/fibre/ante"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	voied25519 "github.com/oasisprotocol/curve25519-voi/primitives/ed25519"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of the fibre MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

// DepositToEscrow deposits funds to the signer's escrow account
func (ms msgServer) DepositToEscrow(goCtx context.Context, msg *types.MsgDepositToEscrow) (*types.MsgDepositToEscrowResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Convert signer address
	signerAddr, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidAddress, "invalid signer address: %s", err)
	}

	// Get or create escrow account
	escrowAccount, found := ms.GetEscrowAccount(ctx, msg.Signer)
	if !found {
		escrowAccount = types.EscrowAccount{
			Signer:           msg.Signer,
			Balance:          sdk.NewCoin(msg.Amount.Denom, math.ZeroInt()),
			AvailableBalance: sdk.NewCoin(msg.Amount.Denom, math.ZeroInt()),
		}
	}

	// Transfer funds from user to module
	if err := ms.bankKeeper.SendCoinsFromAccountToModule(ctx, signerAddr, types.ModuleName, sdk.NewCoins(msg.Amount)); err != nil {
		return nil, errorsmod.Wrapf(err, "failed to transfer funds to escrow")
	}

	// Update escrow account balances
	escrowAccount.Balance = escrowAccount.Balance.Add(msg.Amount)
	escrowAccount.AvailableBalance = escrowAccount.AvailableBalance.Add(msg.Amount)

	// Save the updated escrow account
	ms.SetEscrowAccount(ctx, escrowAccount)

	// Emit event
	event := types.NewEventDepositToEscrow(msg.Signer, msg.Amount)
	if err := ctx.EventManager().EmitTypedEvent(event); err != nil {
		return nil, err
	}

	return &types.MsgDepositToEscrowResponse{}, nil
}

// RequestWithdrawal requests withdrawal from the signer's escrow account
func (ms msgServer) RequestWithdrawal(goCtx context.Context, msg *types.MsgRequestWithdrawal) (*types.MsgRequestWithdrawalResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Get escrow account
	escrowAccount, found := ms.GetEscrowAccount(ctx, msg.Signer)
	if !found {
		return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "escrow account not found for signer: %s", msg.Signer)
	}

	// Check if sufficient available balance
	if escrowAccount.AvailableBalance.IsLT(msg.Amount) {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "insufficient available balance: have %s, need %s", escrowAccount.AvailableBalance, msg.Amount)
	}

	// Get withdrawal delay from params
	params := ms.GetParams(ctx)
	requestedTimestamp := ctx.BlockTime()

	// Verify no existing withdrawal request at current timestamp (prevents key collision)
	_, existing := ms.GetWithdrawal(ctx, msg.Signer, requestedTimestamp)
	if existing {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "withdrawal request already exists for signer %s at timestamp %v", msg.Signer, requestedTimestamp)
	}

	availableTimestamp := requestedTimestamp.Add(params.WithdrawalDelay)

	// Create withdrawal request with available timestamp
	withdrawal := types.Withdrawal{
		Signer:             msg.Signer,
		Amount:             msg.Amount,
		RequestedTimestamp: requestedTimestamp,
		AvailableTimestamp: availableTimestamp,
	}

	// Update escrow account available balance (lock the funds)
	escrowAccount.AvailableBalance = escrowAccount.AvailableBalance.Sub(msg.Amount)
	ms.SetEscrowAccount(ctx, escrowAccount)

	// Save withdrawal request to both indexes
	ms.SetWithdrawal(ctx, withdrawal)

	// Emit event
	event := types.NewEventWithdrawFromEscrowRequest(msg.Signer, msg.Amount, requestedTimestamp, availableTimestamp)
	if err := ctx.EventManager().EmitTypedEvent(event); err != nil {
		return nil, err
	}

	return &types.MsgRequestWithdrawalResponse{}, nil
}

// PayForFibre processes a payment promise with validator signatures
func (ms msgServer) PayForFibre(goCtx context.Context, msg *types.MsgPayForFibre) (*types.MsgPayForFibreResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Convert payment promise to internal format
	pp := fibre.PaymentPromise{}
	if err := pp.FromProto(&msg.PaymentPromise); err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "failed to convert payment promise: %s", err)
	}

	// Perform stateless validation (signature verification, format checks, etc.)
	if !fibreante.VerifiedPayForFibre(ctx, msg) {
		if err := pp.Validate(); err != nil {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "payment promise validation failed: %s", err)
		}
	}

	// Perform stateful verification (escrow account, balance, not already processed)
	var validated validatedPromiseState
	_, err := ms.validatePaymentPromiseStatefulInternal(ctx, &msg.PaymentPromise, false, &validated)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "payment promise stateful verification failed: %s", err)
	}

	promiseHash := validated.promiseHash
	if promiseHash == nil {
		promiseHash, err = pp.Hash()
		if err != nil {
			return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "failed to hash payment promise: %s", err)
		}
	}

	escrowAccount := validated.escrowAccount

	// Calculate payment amount based on blob size and gas per byte
	paymentAmount := ms.calculatePaymentAmount(ctx, msg.PaymentPromise.BlobSize)

	// Deduct payment from escrow account (may reduce pending withdrawals if needed)
	if err := ms.deductPaymentFromEscrow(ctx, &escrowAccount, paymentAmount); err != nil {
		return nil, err
	}

	// Record processed payment
	processedPayment := types.ProcessedPayment{
		PaymentPromiseHash: promiseHash,
		ProcessedAt:        ctx.BlockTime(),
	}
	ms.SetProcessedPayment(ctx, processedPayment)

	// Proposal events are discarded; FinalizeBlock emits the committed event.
	if ctx.ExecMode() != sdk.ExecModeProcessProposal && ctx.ExecMode() != sdk.ExecModePrepareProposal {
		signerAddr := sdk.AccAddress(msg.PaymentPromise.SignerPublicKey.Address()).String()
		event := types.NewEventPayForFibre(signerAddr, msg.PaymentPromise.Namespace, msg.PaymentPromise.Commitment, uint32(len(msg.ValidatorSignatures)))
		if err := ctx.EventManager().EmitTypedEvent(event); err != nil {
			return nil, err
		}
	}

	return &types.MsgPayForFibreResponse{}, nil
}

// ValidatePayForFibreSignatures verifies the payment promise and validator signatures.
func (k Keeper) ValidatePayForFibreSignatures(ctx sdk.Context, msg *types.MsgPayForFibre) error {
	pp := fibre.PaymentPromise{}
	if err := pp.FromProto(&msg.PaymentPromise); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "failed to convert payment promise: %s", err)
	}
	if err := pp.Validate(); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "payment promise validation failed: %s", err)
	}
	signBytes, err := pp.SignBytes()
	if err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "failed to get validator sign bytes: %s", err)
	}
	if err := k.validateValidatorSignatures(ctx, signBytes, msg.PaymentPromise.Height, msg.ValidatorSignatures); err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "validator signature validation failed: %s", err)
	}
	return nil
}

// PaymentPromiseTimeout processes a payment promise after the timeout period
func (ms msgServer) PaymentPromiseTimeout(goCtx context.Context, msg *types.MsgPaymentPromiseTimeout) (*types.MsgPaymentPromiseTimeoutResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Convert payment promise to internal format
	pp := fibre.PaymentPromise{}
	if err := pp.FromProto(&msg.PaymentPromise); err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "failed to convert payment promise: %s", err)
	}

	// Perform stateless validation (signature verification, format checks, etc.)
	if err := pp.Validate(); err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "payment promise validation failed: %s", err)
	}

	// Perform stateful verification (escrow account, balance, not already processed)
	// Use ValidatePaymentPromiseStatefulForTimeout which allows expired promises
	expirationTime, err := ms.ValidatePaymentPromiseStatefulForTimeout(ctx, &msg.PaymentPromise)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "payment promise stateful verification failed: %s", err)
	}

	// Check if timeout period has passed
	currentTime := ctx.BlockTime()
	if currentTime.Before(expirationTime) {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "payment promise has not yet timed out. Timeout at: %s, current time: %s", expirationTime, currentTime)
	}

	// Calculate payment amount based on blob size and gas per byte (same as PayForFibre)
	paymentAmount := ms.calculatePaymentAmount(ctx, msg.PaymentPromise.BlobSize)

	promiseHash, err := pp.Hash()
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "failed to hash payment promise: %s", err)
	}

	// Get escrow account for the payment promise signer
	signerPubKey := msg.PaymentPromise.SignerPublicKey
	escrowSigner := sdk.AccAddress(signerPubKey.Address()).String()

	escrowAccount, found := ms.GetEscrowAccount(ctx, escrowSigner)
	if !found {
		return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "escrow account not found for signer: %s", escrowSigner)
	}

	// Deduct payment from escrow account (may reduce pending withdrawals if needed)
	if err := ms.deductPaymentFromEscrow(ctx, &escrowAccount, paymentAmount); err != nil {
		return nil, err
	}

	// Record processed payment (timeout)
	processedPayment := types.ProcessedPayment{
		PaymentPromiseHash: promiseHash,
		ProcessedAt:        ctx.BlockTime(),
	}
	ms.SetProcessedPayment(ctx, processedPayment)

	// Emit event
	event := types.NewEventPaymentPromiseTimeout(msg.Signer, escrowSigner, promiseHash)
	if err := ctx.EventManager().EmitTypedEvent(event); err != nil {
		return nil, err
	}

	return &types.MsgPaymentPromiseTimeoutResponse{}, nil
}

// UpdateFibreParams updates the fibre module parameters
func (ms msgServer) UpdateFibreParams(goCtx context.Context, msg *types.MsgUpdateFibreParams) (*types.MsgUpdateFibreParamsResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	// Check if the signer is the module authority
	if msg.Authority != ms.GetAuthority() {
		return nil, errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "invalid authority; expected %s, got %s", ms.GetAuthority(), msg.Authority)
	}

	// Validate parameters before setting them
	if err := msg.Params.Validate(); err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid parameters: %s", err)
	}

	// Set the new parameters
	ms.SetParams(ctx, msg.Params)

	// Emit event
	event := types.NewEventUpdateFibreParams(msg.Authority, msg.Params)
	if err := ctx.EventManager().EmitTypedEvent(event); err != nil {
		return nil, err
	}

	return &types.MsgUpdateFibreParamsResponse{}, nil
}

// deductPaymentFromEscrow deducts the payment amount from an escrow account.
// If AvailableBalance is not enough but Balance covers the payment, it reduces pending
// withdrawals (FIFO) by the shortfall amount.
func (ms msgServer) deductPaymentFromEscrow(ctx sdk.Context, escrowAccount *types.EscrowAccount, paymentAmount sdk.Coin) error {
	if escrowAccount.Balance.IsLT(paymentAmount) {
		return errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "insufficient balance: have %s, need %s", escrowAccount.Balance, paymentAmount)
	}

	// availableDeduction = min(AvailableBalance, paymentAmount)
	availableDeduction := paymentAmount
	if escrowAccount.AvailableBalance.IsLT(paymentAmount) {
		availableDeduction = escrowAccount.AvailableBalance
	}

	// Deduct the full payment from the total balance (guaranteed sufficient by the check above).
	escrowAccount.Balance = escrowAccount.Balance.Sub(paymentAmount)
	// Only deduct what AvailableBalance can cover; the rest is locked in pending withdrawals.
	escrowAccount.AvailableBalance = escrowAccount.AvailableBalance.Sub(availableDeduction)
	ms.SetEscrowAccount(ctx, *escrowAccount)

	// If AvailableBalance couldn't cover the full payment, cancel/reduce pending withdrawals (FIFO)
	// by the shortfall amount so that Balance and AvailableBalance stay consistent.
	shortfall := paymentAmount.Sub(availableDeduction)
	if shortfall.IsPositive() {
		if err := ms.ReduceWithdrawalsForPayment(ctx, escrowAccount.Signer, shortfall); err != nil {
			return err
		}
	}

	// Route the settled payment to the fee collector so it is distributed to
	// validators and delegators like a regular data-availability fee. The
	// escrow accounting above only shrinks Balance/AvailableBalance; without
	// this transfer the coins would stay stranded in the module account and be
	// paid to no one. The full payment leaves the module account regardless of
	// how it was split between available balance and pending withdrawals.
	if err := ms.bankKeeper.SendCoinsFromModuleToModule(ctx, types.ModuleName, authtypes.FeeCollectorName, sdk.NewCoins(paymentAmount)); err != nil {
		return errorsmod.Wrap(err, "failed to send fibre payment to fee collector")
	}

	return nil
}

// calculatePaymentAmount calculates the payment amount for a fibre blob based on its size.
// TODO: this assumes 1 utia per gas which may not be correct.
func (ms msgServer) calculatePaymentAmount(_ sdk.Context, blobSize uint32) sdk.Coin {
	return types.PaymentAmount(blobSize)
}

// EstimateGasForPayForFibre estimates the gas required for a PayForFibre message.
// It delegates to [types.EstimateGasForPayForFibre], the shared source of truth for
// the gas formula (also used by the client-side escrow accounting).
func EstimateGasForPayForFibre(blobSize uint32) uint64 {
	return types.EstimateGasForPayForFibre(blobSize)
}

// validateValidatorSignatures checks signatures against the validator set at height.
func (k Keeper) validateValidatorSignatures(ctx sdk.Context, signBytes []byte, height int64, signatures [][]byte) error {
	converted, err := k.validatorSetForSignatures(ctx, height)
	if err != nil {
		return err
	}
	cmtValidators := converted.validators

	// The list is positional over the validator set, so more entries than
	// validators is malformed. Rejecting up front also guarantees every index is
	// in range, so the quorum short-circuit below cannot skip a trailing entry.
	if len(signatures) > len(cmtValidators) {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "signature count %d exceeds validator count %d", len(signatures), len(cmtValidators))
	}
	if _, preverify := ctx.Value(preverifyValsetCacheKey{}).(*preverifyValsetCache); preverify {
		return validateExpandedPositionalSignatures(converted, signBytes, signatures)
	}
	if ctx.IsCheckTx() && len(signatures) >= 16 {
		return validateCheckTxPositionalSignatures(converted, signBytes, signatures)
	}
	valSet := validator.Set{
		ValidatorSet: converted.set,
		Height:       uint64(height),
	}
	twoThirds := cmtmath.Fraction{Numerator: 2, Denominator: 3}
	sigSet := valSet.NewSignatureSet(twoThirds, signBytes)

	// Add all provided signatures to the signature set
	for i, signature := range signatures {
		if len(signature) == 0 {
			continue // Skip empty signatures
		}

		// Add signature to set (this validates the signature internally)
		hasEnough, err := sigSet.Add(cmtValidators[i], signature)
		if err != nil {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid signature at index %d: %s", i, err)
		}
		if hasEnough {
			return nil
		}
	}

	// Check if thresholds are met
	_, err = sigSet.Signatures()
	if err != nil {
		return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "signature validation failed: %s", err)
	}

	return nil
}

// validateExpandedPositionalSignatures preserves SignatureSet's validation
// order and quorum short circuit. The proposal-scoped key expansions avoid a
// contended global LRU lookup for each signature across concurrent workers.
func validateExpandedPositionalSignatures(converted *convertedValset, signBytes []byte, signatures [][]byte) error {
	expanded := converted.expandedKeys()
	required := converted.set.TotalVotingPower() * 2 / 3
	var power int64
	options := &voied25519.Options{Verify: voied25519.VerifyOptionsStdLib}
	for i, signature := range signatures {
		if len(signature) == 0 {
			continue
		}
		if expanded[i] == nil || !voied25519.VerifyExpandedWithOptions(expanded[i], signBytes, signature, options) {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid signature at index %d: invalid signature from validator %s", i, converted.validators[i].Address.String())
		}
		power += converted.validators[i].VotingPower
		if power >= required {
			return nil
		}
	}
	return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "signature validation failed: %s", (&validator.NotEnoughSignaturesError{CollectedPower: power, RequiredPower: required}).Error())
}

// CheckTx processes transactions serially, but the positional validator
// signatures within one transaction are independent. Verify them in parallel
// and apply the original quorum and first-invalid rules in index order.
func validateCheckTxPositionalSignatures(converted *convertedValset, signBytes []byte, signatures [][]byte) error {
	valid := make([]bool, len(signatures))
	workers := min(runtime.NumCPU(), 16, len(signatures))
	var wg sync.WaitGroup
	for worker := range workers {
		wg.Go(func() {
			defer func() { _ = recover() }()
			for i := worker; i < len(signatures); i += workers {
				if len(signatures[i]) != 0 {
					valid[i] = validator.VerifySignature(converted.validators[i], signBytes, signatures[i])
				}
			}
		})
	}
	wg.Wait()
	required := converted.set.TotalVotingPower() * 2 / 3
	var power int64
	for i, signature := range signatures {
		if len(signature) == 0 {
			continue
		}
		if !valid[i] {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid signature at index %d: invalid signature from validator %s", i, converted.validators[i].Address.String())
		}
		power += converted.validators[i].VotingPower
		if power >= required {
			return nil
		}
	}
	return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "signature validation failed: %s", (&validator.NotEnoughSignaturesError{CollectedPower: power, RequiredPower: required}).Error())
}

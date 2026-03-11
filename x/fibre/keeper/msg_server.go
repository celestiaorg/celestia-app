package keeper

import (
	"context"

	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v8/fibre"
	"github.com/celestiaorg/celestia-app/v8/fibre/validator"
	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	"github.com/cometbft/cometbft/crypto/ed25519"
	cmtmath "github.com/cometbft/cometbft/libs/math"
	core "github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
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
	if err := pp.Validate(); err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "payment promise validation failed: %s", err)
	}

	// Perform stateful verification (escrow account, balance, not already processed)
	_, err := ms.ValidatePaymentPromiseStateful(ctx, &msg.PaymentPromise)
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "payment promise stateful verification failed: %s", err)
	}

	// Validate validator signatures
	validatorSignBytes, err := pp.SignBytesValidator()
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "failed to get validator sign bytes: %s", err)
	}

	if err := ms.validateValidatorSignatures(ctx, validatorSignBytes, msg.PaymentPromise.Height, msg.ValidatorSignatures); err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "validator signature validation failed: %s", err)
	}

	promiseHash, err := pp.Hash()
	if err != nil {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "failed to hash payment promise: %s", err)
	}

	// Get escrow account for the payment promise signer
	signerPubKey := msg.PaymentPromise.SignerPublicKey
	signerAddr := sdk.AccAddress(signerPubKey.Address()).String()

	escrowAccount, found := ms.GetEscrowAccount(ctx, signerAddr)
	if !found {
		return nil, errorsmod.Wrapf(sdkerrors.ErrNotFound, "escrow account not found for signer: %s", signerAddr)
	}

	// Calculate payment amount based on blob size and gas per byte
	paymentAmount := ms.calculatePaymentAmount(ctx, msg.PaymentPromise.BlobSize)

	// Check if sufficient balance
	if escrowAccount.Balance.IsLT(paymentAmount) {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "insufficient balance: have %s, need %s", escrowAccount.Balance, paymentAmount)
	}
	// Check if sufficient available balance
	if escrowAccount.AvailableBalance.IsLT(paymentAmount) {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "insufficient available balance: have %s, need %s", escrowAccount.AvailableBalance, paymentAmount)
	}

	// Deduct payment from escrow account
	escrowAccount.Balance = escrowAccount.Balance.Sub(paymentAmount)
	escrowAccount.AvailableBalance = escrowAccount.AvailableBalance.Sub(paymentAmount)
	ms.SetEscrowAccount(ctx, escrowAccount)

	// Record processed payment
	processedPayment := types.ProcessedPayment{
		PaymentPromiseHash: promiseHash,
		ProcessedAt:        ctx.BlockTime(),
	}
	ms.SetProcessedPayment(ctx, processedPayment)

	// Emit event
	event := types.NewEventPayForFibre(signerAddr, msg.PaymentPromise.Namespace, msg.PaymentPromise.Commitment, uint32(len(msg.ValidatorSignatures)))
	if err := ctx.EventManager().EmitTypedEvent(event); err != nil {
		return nil, err
	}

	return &types.MsgPayForFibreResponse{}, nil
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

	// Check if sufficient balance (defensive check to prevent panic on Sub)
	if escrowAccount.Balance.IsLT(paymentAmount) {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "insufficient balance: have %s, need %s", escrowAccount.Balance, paymentAmount)
	}
	// Check if sufficient available balance (should already be validated, but double-check)
	if escrowAccount.AvailableBalance.IsLT(paymentAmount) {
		return nil, errorsmod.Wrapf(sdkerrors.ErrInsufficientFunds, "insufficient available balance: have %s, need %s", escrowAccount.AvailableBalance, paymentAmount)
	}

	// Deduct payment from escrow account (both balance and available_balance)
	escrowAccount.Balance = escrowAccount.Balance.Sub(paymentAmount)
	escrowAccount.AvailableBalance = escrowAccount.AvailableBalance.Sub(paymentAmount)
	ms.SetEscrowAccount(ctx, escrowAccount)

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

	// Set the new parameters
	ms.SetParams(ctx, msg.Params)

	// Emit event
	event := types.NewEventUpdateFibreParams(msg.Authority, msg.Params)
	if err := ctx.EventManager().EmitTypedEvent(event); err != nil {
		return nil, err
	}

	return &types.MsgUpdateFibreParamsResponse{}, nil
}

// calculatePaymentAmount calculates the payment amount for a fibre blob based on its size and gas parameters
func (ms msgServer) calculatePaymentAmount(ctx sdk.Context, blobSize uint32) sdk.Coin {
	params := ms.GetParams(ctx)
	// TODO: this assumes 1 utia per gas which may not be correct.
	return CalculatePaymentAmount(blobSize, params.GasPerBlobByte)
}

// CalculatePaymentAmount computes the payment coin from blobSize and gasPerBlobByte.
// Both operands are widened to uint64 before multiplication to prevent uint32 overflow.
func CalculatePaymentAmount(blobSize, gasPerBlobByte uint32) sdk.Coin {
	result := uint64(blobSize) * uint64(gasPerBlobByte)
	return sdk.NewCoin(appconsts.BondDenom, math.NewIntFromUint64(result))
}

// validateValidatorSignatures validates validator signatures using the existing SignatureSet infrastructure
func (ms msgServer) validateValidatorSignatures(ctx sdk.Context, validatorSignBytes []byte, height int64, signatures [][]byte) error {
	// Get historical validator set at the height
	historicalInfo, err := ms.stakingKeeper.GetHistoricalInfo(ctx, height)
	if err != nil {
		return errorsmod.Wrapf(err, "failed to get historical validator set at height %d", height)
	}

	// Convert SDK validators to CometBFT validators
	cmtValidators := make([]*core.Validator, len(historicalInfo.Valset))
	for i, val := range historicalInfo.Valset {
		consPubKey, err := val.ConsPubKey()
		if err != nil {
			return errorsmod.Wrapf(err, "failed to get consensus public key for validator %s", val.GetOperator())
		}

		// Create CometBFT ed25519 public key from bytes
		pubKeyBytes := consPubKey.Bytes()
		if len(pubKeyBytes) != ed25519.PubKeySize {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "invalid ed25519 public key size for validator %s", val.GetOperator())
		}

		cmtPubKey := ed25519.PubKey(pubKeyBytes)
		cmtValidators[i] = core.NewValidator(cmtPubKey, val.Tokens.Int64())
	}

	// Create validator set
	cmtValSet := core.NewValidatorSet(cmtValidators)
	valSet := validator.Set{
		ValidatorSet: cmtValSet,
		Height:       uint64(height),
	}

	// Create signature set with 2/3+ thresholds
	twoThirds := cmtmath.Fraction{Numerator: 2, Denominator: 3}
	sigSet := valSet.NewSignatureSet(twoThirds, validatorSignBytes)

	// Add all provided signatures to the signature set
	for i, signature := range signatures {
		if len(signature) == 0 {
			continue // Skip empty signatures
		}

		if i >= len(cmtValidators) {
			return errorsmod.Wrapf(sdkerrors.ErrInvalidRequest, "signature index %d exceeds validator count %d", i, len(cmtValidators))
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

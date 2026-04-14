package keeper

import (
	"fmt"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v9/fibre"
	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Keeper handles all the state changes for the fibre module.
type Keeper struct {
	cdc           codec.Codec
	storeKey      storetypes.StoreKey
	bankKeeper    types.BankKeeper
	stakingKeeper types.StakingKeeper
	// authority is the address that has the authority to update module parameters.
	// This is typically the governance module address.
	authority string
}

// NewKeeper creates a new fibre Keeper instance
func NewKeeper(cdc codec.Codec, storeKey storetypes.StoreKey, bankKeeper types.BankKeeper, stakingKeeper types.StakingKeeper, authority string) *Keeper {
	return &Keeper{
		cdc:           cdc,
		storeKey:      storeKey,
		bankKeeper:    bankKeeper,
		stakingKeeper: stakingKeeper,
		authority:     authority,
	}
}

// GetAuthority returns the fibre module's authority.
func (k Keeper) GetAuthority() string {
	return k.authority
}

// Logger returns a x/fibre specific logger.
func (k Keeper) Logger(ctx sdk.Context) log.Logger {
	return ctx.Logger().With("module", fmt.Sprintf("x/%s", types.ModuleName))
}

// GetParams returns the x/fibre module's parameters.
func (k Keeper) GetParams(ctx sdk.Context) types.Params {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get([]byte(types.ParamsKey))
	if len(bz) == 0 {
		return types.DefaultParams()
	}

	var params types.Params
	k.cdc.MustUnmarshal(bz, &params)
	return params
}

// SetParams sets the params
func (k Keeper) SetParams(ctx sdk.Context, params types.Params) {
	store := ctx.KVStore(k.storeKey)
	bz := k.cdc.MustMarshal(&params)
	store.Set([]byte(types.ParamsKey), bz)
}

// GetEscrowAccount retrieves an escrow account by signer address.
func (k Keeper) GetEscrowAccount(ctx sdk.Context, signer string) (account types.EscrowAccount, isFound bool) {
	store := ctx.KVStore(k.storeKey)
	key := types.EscrowAccountKey(signer)
	bz := store.Get(key)
	if bz == nil {
		return types.EscrowAccount{}, false
	}

	k.cdc.MustUnmarshal(bz, &account)
	return account, true
}

// SetEscrowAccount stores an escrow account
func (k Keeper) SetEscrowAccount(ctx sdk.Context, account types.EscrowAccount) {
	store := ctx.KVStore(k.storeKey)
	key := types.EscrowAccountKey(account.Signer)
	bz := k.cdc.MustMarshal(&account)
	store.Set(key, bz)
}

// DeleteEscrowAccount removes an escrow account from the store
func (k Keeper) DeleteEscrowAccount(ctx sdk.Context, signer string) {
	store := ctx.KVStore(k.storeKey)
	key := types.EscrowAccountKey(signer)
	store.Delete(key)
}

// GetWithdrawal retrieves a withdrawal by signer and timestamp
func (k Keeper) GetWithdrawal(ctx sdk.Context, signer string, requestedTimestamp time.Time) (withdrawal types.Withdrawal, isFound bool) {
	store := ctx.KVStore(k.storeKey)
	key := types.WithdrawalsBySignerKey(signer, requestedTimestamp)
	bz := store.Get(key)
	if bz == nil {
		return types.Withdrawal{}, false
	}

	k.cdc.MustUnmarshal(bz, &withdrawal)
	return withdrawal, true
}

// SetWithdrawal saves a withdrawal to both indexes:
// 1. Primary index: withdrawals_by_signer/{signer}/{requested_timestamp}
// 2. Secondary index: withdrawals_by_available/{available_timestamp}/{signer}
func (k Keeper) SetWithdrawal(ctx sdk.Context, withdrawal types.Withdrawal) {
	store := ctx.KVStore(k.storeKey)
	bz := k.cdc.MustMarshal(&withdrawal)

	// Store in primary index
	primaryKey := types.WithdrawalsBySignerKey(withdrawal.Signer, withdrawal.RequestedTimestamp)
	store.Set(primaryKey, bz)

	// Store in secondary index
	secondaryKey := types.WithdrawalsByAvailableKey(withdrawal.AvailableTimestamp, withdrawal.Signer)
	store.Set(secondaryKey, bz)
}

// DeleteWithdrawal removes a withdrawal from both indexes.
// This should be called when a withdrawal is processed or cancelled.
func (k Keeper) DeleteWithdrawal(ctx sdk.Context, withdrawal types.Withdrawal) {
	store := ctx.KVStore(k.storeKey)

	// Delete from primary index
	primaryKey := types.WithdrawalsBySignerKey(withdrawal.Signer, withdrawal.RequestedTimestamp)
	store.Delete(primaryKey)

	// Delete from secondary index
	secondaryKey := types.WithdrawalsByAvailableKey(withdrawal.AvailableTimestamp, withdrawal.Signer)
	store.Delete(secondaryKey)
}

// GetWithdrawalsBySigner retrieves all withdrawals for a signer
func (k Keeper) GetWithdrawalsBySigner(ctx sdk.Context, signer string) []types.Withdrawal {
	store := ctx.KVStore(k.storeKey)
	prefix := types.WithdrawalsBySignerPrefix(signer)
	iterator := storetypes.KVStorePrefixIterator(store, prefix)
	defer iterator.Close()

	var withdrawals []types.Withdrawal
	for ; iterator.Valid(); iterator.Next() {
		var withdrawal types.Withdrawal
		k.cdc.MustUnmarshal(iterator.Value(), &withdrawal)
		withdrawals = append(withdrawals, withdrawal)
	}

	return withdrawals
}

// GetWithdrawalsByAvailableIterator returns an iterator for all withdrawals available up to the given time
func (k Keeper) GetWithdrawalsByAvailableIterator(ctx sdk.Context, upToTime time.Time) storetypes.Iterator {
	store := ctx.KVStore(k.storeKey)
	// Start from the beginning of the withdrawals-by-available index
	start := types.WithdrawalsByAvailableKeyPrefix
	// End at the last possible key for the given time
	end := storetypes.PrefixEndBytes(types.WithdrawalsByAvailablePrefix(upToTime))
	return store.Iterator(start, end)
}

// ParseWithdrawalsByAvailableKey parses the available_at timestamp and signer from the key
func (k Keeper) ParseWithdrawalsByAvailableKey(key []byte) (available time.Time, signer string, err error) {
	// Remove the prefix
	key = key[len(types.WithdrawalsByAvailableKeyPrefix):]

	// Parse the timestamp (first 29 bytes as per SDK's FormatTimeBytes)
	timestampBytes := key[:29]

	available, err = sdk.ParseTimeBytes(timestampBytes)
	if err != nil {
		return time.Time{}, "", fmt.Errorf("failed to parse timestamp: %w", err)
	}

	// Skip the separator "/"
	key = key[29:]
	if len(key) > 0 && key[0] == '/' {
		key = key[1:]
	}

	// The rest is the signer address
	signer = string(key)
	return available, signer, nil
}

// GetProcessedPayment retrieves a processed payment by promiseHash
func (k Keeper) GetProcessedPayment(ctx sdk.Context, promiseHash []byte) (payment types.ProcessedPayment, isFound bool) {
	store := ctx.KVStore(k.storeKey)
	key := types.ProcessedPaymentsByHashKey(promiseHash)
	bz := store.Get(key)
	if bz == nil {
		return types.ProcessedPayment{}, false
	}
	k.cdc.MustUnmarshal(bz, &payment)
	return payment, true
}

// SetProcessedPayment saves a processed payment to both indexes:
// 1. Primary index: processed_payments_by_hash/{hash}
// 2. Secondary index: processed_payments_by_time/{processed_at}/{hash}
func (k Keeper) SetProcessedPayment(ctx sdk.Context, payment types.ProcessedPayment) {
	store := ctx.KVStore(k.storeKey)
	bz := k.cdc.MustMarshal(&payment)

	// Store in primary index (by hash)
	primaryKey := types.ProcessedPaymentsByHashKey(payment.PaymentPromiseHash)
	store.Set(primaryKey, bz)

	// Store in secondary index (by time)
	secondaryKey := types.ProcessedPaymentsByTimeKey(payment.ProcessedAt, payment.PaymentPromiseHash)
	store.Set(secondaryKey, bz)
}

// DeleteProcessedPayment removes a processed payment from both indexes.
// This should be called when pruning old processed payments.
func (k Keeper) DeleteProcessedPayment(ctx sdk.Context, payment types.ProcessedPayment) {
	store := ctx.KVStore(k.storeKey)

	// Delete from primary index
	primaryKey := types.ProcessedPaymentsByHashKey(payment.PaymentPromiseHash)
	store.Delete(primaryKey)

	// Delete from secondary index
	secondaryKey := types.ProcessedPaymentsByTimeKey(payment.ProcessedAt, payment.PaymentPromiseHash)
	store.Delete(secondaryKey)
}

// IsPaymentPromiseProcessed returns true if a payment has been processed for the given promise.
func (k Keeper) IsPaymentPromiseProcessed(ctx sdk.Context, promise *types.PaymentPromise) bool {
	store := ctx.KVStore(k.storeKey)
	pp := fibre.PaymentPromise{}
	if err := pp.FromProto(promise); err != nil {
		return false
	}
	hash, err := pp.Hash()
	if err != nil {
		return false
	}
	key := types.ProcessedPaymentsByHashKey(hash)
	return store.Has(key)
}

// IsPaymentProcessedByHash returns true if a payment has been processed for the given promise hash.
func (k Keeper) IsPaymentProcessedByHash(ctx sdk.Context, promiseHash []byte) bool {
	store := ctx.KVStore(k.storeKey)
	key := types.ProcessedPaymentsByHashKey(promiseHash)
	return store.Has(key)
}

// ValidatePaymentPromiseInternal validates a payment promise and returns an error if the promise is invalid.
// It performs both stateless and stateful validation.
func (k Keeper) ValidatePaymentPromiseInternal(ctx sdk.Context, promise *types.PaymentPromise) error {
	// Perform stateless validation
	if err := k.ValidatePaymentPromiseStateless(ctx, promise); err != nil {
		return err
	}

	// Perform stateful validation
	_, err := k.ValidatePaymentPromiseStateful(ctx, promise)
	return err
}

func (k Keeper) ValidatePaymentPromiseStateless(ctx sdk.Context, promise *types.PaymentPromise) error {
	pp := fibre.PaymentPromise{}
	if err := pp.FromProto(promise); err != nil {
		return fmt.Errorf("invalid payment promise format: %v", err)
	}

	return pp.Validate()
}

// GetProcessedPaymentsByTimeIterator returns an iterator for all processed payments up to the given time
func (k Keeper) GetProcessedPaymentsByTimeIterator(ctx sdk.Context, upToTime time.Time) storetypes.Iterator {
	store := ctx.KVStore(k.storeKey)
	// Start from the beginning of the processed-payments-by-time index
	start := types.ProcessedPaymentsByTimeKeyPrefix
	// End at the last possible key for the given time
	end := storetypes.PrefixEndBytes(types.ProcessedPaymentsByTimePrefix(upToTime))
	return store.Iterator(start, end)
}

// ParseProcessedPaymentsByTimeKey parses the processed_at timestamp and payment promise hash from the key
func (k Keeper) ParseProcessedPaymentsByTimeKey(key []byte) (processedAt time.Time, paymentPromiseHash []byte, err error) {
	// Remove the prefix
	key = key[len(types.ProcessedPaymentsByTimeKeyPrefix):]

	// Parse the timestamp (first 29 bytes as per SDK's FormatTimeBytes)
	timestampBytes := key[:29]

	processedAt, err = sdk.ParseTimeBytes(timestampBytes)
	if err != nil {
		return time.Time{}, nil, fmt.Errorf("failed to parse timestamp: %w", err)
	}

	// Skip the separator "/"
	key = key[29:]
	if len(key) > 0 && key[0] == '/' {
		key = key[1:]
	}

	// The rest is the payment promise hash
	paymentPromiseHash = key
	return processedAt, paymentPromiseHash, nil
}

// validatePaymentPromiseStatefulInternal performs the core stateful validation logic.
// The isTimeout parameter indicates whether this is being called for timeout processing,
// which skips expiration and height validation to allow processing older promises.
func (k Keeper) validatePaymentPromiseStatefulInternal(ctx sdk.Context, promise *types.PaymentPromise, isTimeout bool) (time.Time, error) {
	params := k.GetParams(ctx)
	currentTime := ctx.BlockTime()
	creationTime := promise.CreationTimestamp

	// Check creation_timestamp is not too old (must be greater than header_timestamp - withdrawal_delay)
	minAllowedTime := currentTime.Add(-params.WithdrawalDelay)
	if !creationTime.After(minAllowedTime) {
		return time.Time{}, fmt.Errorf("creation_timestamp %v must be greater than %v (current_time - withdrawal_delay)", creationTime, minAllowedTime)
	}

	expirationTime := creationTime.Add(params.PaymentPromiseTimeout)
	// Expiration time validation only applies to normal flow (not timeout mechanism)
	if !isTimeout {
		if currentTime.After(expirationTime) || currentTime.Equal(expirationTime) {
			return time.Time{}, fmt.Errorf("payment promise expired: creation_timestamp %v + timeout %v = %v, current_time: %v", creationTime, params.PaymentPromiseTimeout, expirationTime, currentTime)
		}
	}

	// Height validation only applies to normal flow (not timeout mechanism)
	if !isTimeout {
		currentHeight := ctx.BlockHeight()
		promiseHeight := promise.Height

		// Validate height is not too far in the past
		if currentHeight-promiseHeight > int64(params.PaymentPromiseHeightWindow) {
			return time.Time{}, fmt.Errorf("payment promise height %d is too far in the past (current height: %d, max window: %d)", promiseHeight, currentHeight, params.PaymentPromiseHeightWindow)
		}

		// Validate height is not too far in the future (allow up to 1 block ahead)
		if promiseHeight > currentHeight+1 {
			return time.Time{}, fmt.Errorf("payment promise height %d is too far in the future (current height: %d, max allowed: %d)", promiseHeight, currentHeight, currentHeight+1)
		}
	}

	// Check if payment promise has already been processed
	if isAlreadyProcessed := k.IsPaymentPromiseProcessed(ctx, promise); isAlreadyProcessed {
		return time.Time{}, fmt.Errorf("payment promise has already been processed")
	}

	// Check escrow account exists
	signerAddr := sdk.AccAddress(promise.SignerPublicKey.Address())
	signerAddrStr := signerAddr.String()
	escrowAccount, found := k.GetEscrowAccount(ctx, signerAddrStr)
	if !found {
		return time.Time{}, fmt.Errorf("escrow account not found for signer %v", signerAddrStr)
	}

	// Check sufficient balance (includes funds locked in pending withdrawals)
	// TODO: This assumes 1 gas = 1 utia but the minimum gas price could be
	// different.
	gas := EstimateGasForPayForFibre(promise.BlobSize)
	requiredAmount := sdk.NewCoin(appconsts.BondDenom, math.NewIntFromUint64(gas))

	hasSufficientBalance := escrowAccount.Balance.IsGTE(requiredAmount)
	if !hasSufficientBalance {
		return time.Time{}, fmt.Errorf("insufficient balance in escrow account. required: %v, balance: %v", requiredAmount, escrowAccount.Balance)
	}

	return expirationTime, nil
}

// ValidatePaymentPromiseStateful performs stateful validation of a payment promise.
//
// This method does NOT perform stateless validation.
// Callers should perform stateless validation separately via pp.Validate().
//
// Returns the expiration time if validation succeeds.
func (k Keeper) ValidatePaymentPromiseStateful(ctx sdk.Context, promise *types.PaymentPromise) (time.Time, error) {
	isTimeout := false
	return k.validatePaymentPromiseStatefulInternal(ctx, promise, isTimeout)
}

// ValidatePaymentPromiseStatefulForTimeout performs stateful validation of a payment promise for timeout processing.
// It performs the same checks as ValidatePaymentPromiseStateful except it skips expiration and height validation
// to allow processing older promises that may be outside the normal validation windows.
//
// This method does NOT perform stateless validation.
// Callers should perform stateless validation separately via pp.Validate().
//
// Returns the expiration time if validation succeeds.
func (k Keeper) ValidatePaymentPromiseStatefulForTimeout(ctx sdk.Context, promise *types.PaymentPromise) (time.Time, error) {
	isTimeout := true
	return k.validatePaymentPromiseStatefulInternal(ctx, promise, isTimeout)
}

// ReduceWithdrawalsForPayment reduces pending withdrawal amounts for a signer by the given shortfall.
// It iterates withdrawals in order (oldest first) and either partially reduces a withdrawal's
// amount or fully consumes (deletes) it.
// Precondition: expects the escrow.Balance >= remaining. Otherwise, returns an error.
func (k Keeper) ReduceWithdrawalsForPayment(ctx sdk.Context, signer string, remaining sdk.Coin) error {
	store := ctx.KVStore(k.storeKey)
	prefix := types.WithdrawalsBySignerPrefix(signer)
	iterator := storetypes.KVStorePrefixIterator(store, prefix)
	defer iterator.Close()

	for ; iterator.Valid() && remaining.IsPositive(); iterator.Next() {
		var withdrawal types.Withdrawal
		k.cdc.MustUnmarshal(iterator.Value(), &withdrawal)

		if remaining.IsGTE(withdrawal.Amount) {
			remaining = remaining.Sub(withdrawal.Amount)
			k.DeleteWithdrawal(ctx, withdrawal)
		} else {
			withdrawal.Amount = withdrawal.Amount.Sub(remaining)
			k.SetWithdrawal(ctx, withdrawal)
			remaining = sdk.NewCoin(remaining.Denom, math.ZeroInt())
		}
	}

	if remaining.IsPositive() {
		return fmt.Errorf("pending withdrawals for signer %s do not cover the shortfall of %s", signer, remaining)
	}

	return nil
}

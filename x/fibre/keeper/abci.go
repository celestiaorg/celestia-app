package keeper

import (
	"github.com/celestiaorg/celestia-app/v9/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// BeginBlocker processes automatic state transitions at the beginning of each block
func (k Keeper) BeginBlocker(ctx sdk.Context) error {
	// Process available withdrawals first (affects escrow balances)
	if err := k.processAvailableWithdrawals(ctx); err != nil {
		return err
	}

	// Prune processed payments that are outside the retention window
	if err := k.pruneProcessedPayments(ctx); err != nil {
		return err
	}

	return nil
}

// processAvailableWithdrawals transfers funds from escrow to user accounts
// after a withdrawal becomes available.
func (k Keeper) processAvailableWithdrawals(ctx sdk.Context) error {
	currentTime := ctx.BlockTime()

	// Iterate over withdrawals-by-available index starting from earliest timestamp
	iterator := k.GetWithdrawalsByAvailableIterator(ctx, currentTime)
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		// Parse key to extract available_at timestamp and signer address
		availableTime, signer, err := k.ParseWithdrawalsByAvailableKey(iterator.Key())
		if err != nil {
			// Log error but continue processing other withdrawals
			k.Logger(ctx).Error("failed to parse withdrawals-by-available key", "error", err)
			continue
		}

		// Stop if we've reached withdrawals not yet available
		if availableTime.After(currentTime) {
			break
		}

		// Get full withdrawal from value
		var withdrawal types.Withdrawal
		k.cdc.MustUnmarshal(iterator.Value(), &withdrawal)
		amount := withdrawal.Amount

		// Convert signer string to AccAddress
		signerAddr, err := sdk.AccAddressFromBech32(signer)
		if err != nil {
			// Log error but continue processing other withdrawals
			k.Logger(ctx).Error("failed to parse signer address", "error", err, "signer", signer)
			continue
		}

		// Update escrow account balance (decrease total balance)
		escrowAccount, found := k.GetEscrowAccount(ctx, signer)
		if !found {
			// This shouldn't happen, but log and continue
			k.Logger(ctx).Error("escrow account not found during withdrawal processing", "signer", signer)
			continue
		}
		if escrowAccount.Balance.IsLT(amount) {
			// This shouldn't happen, but log and continue
			k.Logger(ctx).Error("escrow account balance is less than withdrawal amount", "signer", signer, "balance", escrowAccount.Balance, "amount", amount)
			continue
		}
		escrowAccount.Balance = escrowAccount.Balance.Sub(amount)
		k.SetEscrowAccount(ctx, escrowAccount)

		// Process withdrawal: transfer from module to user account
		err = k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, signerAddr, sdk.NewCoins(amount))
		if err != nil {
			// Log error but continue processing other withdrawals
			k.Logger(ctx).Error("failed to process withdrawal", "error", err, "signer", signer)
			continue
		}

		// Remove from both withdrawal indexes
		k.DeleteWithdrawal(ctx, withdrawal)

		// Emit event
		event := types.NewEventWithdrawFromEscrowExecuted(signer, amount)
		if err := ctx.EventManager().EmitTypedEvent(event); err != nil {
			// Log error but continue - event emission failure shouldn't stop processing
			k.Logger(ctx).Error("failed to emit withdrawal executed event", "error", err, "signer", signer)
		}
	}

	return nil
}

// pruneProcessedPayments removes processed payments that are outside
// the retention window to prevent unbounded state growth.
func (k Keeper) pruneProcessedPayments(ctx sdk.Context) error {
	currentTime := ctx.BlockTime()
	params := k.GetParams(ctx)

	// Calculate the cutoff time: anything processed before this should be pruned
	cutoffTime := currentTime.Add(-params.PaymentPromiseRetentionWindow)

	// Iterate over processed payments by time, starting from earliest
	iterator := k.GetProcessedPaymentsByTimeIterator(ctx, cutoffTime)
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		// Parse key to extract processed_at timestamp and payment promise hash
		processedAt, paymentPromiseHash, err := k.ParseProcessedPaymentsByTimeKey(iterator.Key())
		if err != nil {
			// Log error but continue pruning other processed payments
			k.Logger(ctx).Error("failed to parse processed-payments-by-time key", "error", err)
			continue
		}

		// Stop if we've reached payments within the retention window
		if processedAt.After(cutoffTime) {
			break
		}

		// Get full processed payment from value
		var processedPayment types.ProcessedPayment
		k.cdc.MustUnmarshal(iterator.Value(), &processedPayment)

		// Delete the processed payment from both indexes
		k.DeleteProcessedPayment(ctx, processedPayment)

		// Emit event for pruned processed payment
		event := types.NewEventProcessedPaymentPruned(paymentPromiseHash, processedAt)
		if err := ctx.EventManager().EmitTypedEvent(event); err != nil {
			// Log error but continue - event emission failure shouldn't stop processing
			k.Logger(ctx).Error("failed to emit processed payment pruned event", "error", err, "hash", paymentPromiseHash)
		}
	}

	return nil
}

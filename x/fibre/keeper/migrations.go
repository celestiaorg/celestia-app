package keeper

import (
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Migrator handles the fibre module's store migrations.
type Migrator struct {
	keeper Keeper
}

// NewMigrator returns a Migrator for the given keeper.
func NewMigrator(keeper Keeper) Migrator {
	return Migrator{keeper: keeper}
}

// Migrate1to2 migrates the fibre store from consensus version 1 to 2. It repairs
// the escrow records that v10 left unreachable:
//
//   - Escrow accounts denominated in anything but utia are refunded to their
//     signer and deleted, together with their pending withdrawals. v10.4.0
//     accepted such deposits, an escrow account takes the denom of its first
//     deposit, and since #8143 rejected non-utia deposits and withdrawals those
//     accounts could neither pay for fibre nor be withdrawn.
//   - Escrow accounts keyed by a non-canonical (e.g. uppercase) bech32 signer
//     string are merged into the canonical record, along with their pending
//     withdrawals. v10.4.0 and v10.5.0 keyed records by the signer string as
//     signed, while settlement always used the canonical form; since #8187
//     deposits and withdrawals use the canonical form too, so those records
//     were unreachable.
func (m Migrator) Migrate1to2(ctx sdk.Context) error {
	return m.keeper.MigrateEscrowAccounts(ctx)
}

// MigrateEscrowAccounts runs the two repairs described in Migrate1to2. Refunds
// run first so that by the time records are merged every remaining account is
// denominated in utia. Records are collected before the store is mutated, and
// every record is re-read from the store right before it is changed, so the
// result depends only on committed state and on store order. A record that
// cannot be repaired is logged and left as it is; the migration never fails
// the upgrade over a single record.
func (k Keeper) MigrateEscrowAccounts(ctx sdk.Context) error {
	for _, account := range k.collectEscrowAccounts(ctx) {
		if account.Balance.Denom == appconsts.BondDenom {
			continue
		}
		k.refundEscrowAccount(ctx, account.Signer)
	}

	for _, account := range k.collectEscrowAccounts(ctx) {
		addr, err := sdk.AccAddressFromBech32(account.Signer)
		if err != nil {
			k.Logger(ctx).Error("fibre migration: escrow signer does not parse, leaving record untouched", "signer", account.Signer, "error", err)
			continue
		}
		canonical := addr.String()
		if account.Signer == canonical {
			continue
		}
		k.mergeEscrowAccount(ctx, account.Signer, canonical)
	}

	return nil
}

// collectEscrowAccounts returns every escrow account in store order.
func (k Keeper) collectEscrowAccounts(ctx sdk.Context) []types.EscrowAccount {
	var accounts []types.EscrowAccount
	k.IterateEscrowAccounts(ctx, func(account types.EscrowAccount) bool {
		accounts = append(accounts, account)
		return false
	})
	return accounts
}

// refundEscrowAccount sends the escrow account's full Balance back to its
// signer and deletes the record and its pending withdrawals. Balance is the
// whole amount the module holds for the account: a pending withdrawal only
// locks AvailableBalance and is subtracted from Balance when it is paid out
// (see processAvailableWithdrawals). The record is left untouched when the
// signer does not parse or the bank transfer fails.
func (k Keeper) refundEscrowAccount(ctx sdk.Context, signer string) {
	logger := k.Logger(ctx)
	account, found := k.GetEscrowAccount(ctx, signer)
	if !found {
		return
	}
	addr, err := sdk.AccAddressFromBech32(account.Signer)
	if err != nil {
		logger.Error("fibre migration: escrow signer does not parse, leaving record untouched", "signer", account.Signer, "error", err)
		return
	}
	if err := account.Balance.Validate(); err != nil {
		logger.Error("fibre migration: escrow balance is invalid, leaving record untouched", "signer", account.Signer, "balance", account.Balance.String(), "error", err)
		return
	}
	if account.Balance.IsPositive() {
		refund := sdk.Coins{account.Balance}
		if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, addr, refund); err != nil {
			logger.Error("fibre migration: refund of non-utia escrow failed, leaving record untouched", "signer", account.Signer, "balance", account.Balance.String(), "error", err)
			return
		}
	}

	withdrawals := k.GetWithdrawalsBySigner(ctx, account.Signer)
	for _, withdrawal := range withdrawals {
		k.DeleteWithdrawal(ctx, withdrawal)
	}
	k.DeleteEscrowAccount(ctx, account.Signer)
	logger.Info("fibre migration: refunded and deleted non-utia escrow account", "signer", account.Signer, "refunded", account.Balance.String(), "withdrawals_deleted", len(withdrawals))
}

// mergeEscrowAccount folds the escrow account stored under signer into the one
// stored under canonical, creating the canonical record if there is none, and
// re-keys the pending withdrawals of signer to canonical. A withdrawal whose
// requested timestamp collides with one already stored under canonical has its
// amount added to the existing one, which keeps its available timestamp. The
// records are left untouched when the two accounts hold different denoms.
func (k Keeper) mergeEscrowAccount(ctx sdk.Context, signer, canonical string) {
	logger := k.Logger(ctx)
	account, found := k.GetEscrowAccount(ctx, signer)
	if !found {
		return
	}
	if account.Balance.Denom != account.AvailableBalance.Denom {
		logger.Error("fibre migration: escrow balance denoms disagree, leaving record untouched", "signer", signer, "balance", account.Balance.String(), "available_balance", account.AvailableBalance.String())
		return
	}

	target, targetFound := k.GetEscrowAccount(ctx, canonical)
	if !targetFound {
		target = types.EscrowAccount{
			Signer:           canonical,
			Balance:          sdk.NewCoin(account.Balance.Denom, account.Balance.Amount),
			AvailableBalance: sdk.NewCoin(account.AvailableBalance.Denom, account.AvailableBalance.Amount),
		}
	} else {
		if target.Balance.Denom != account.Balance.Denom || target.AvailableBalance.Denom != account.AvailableBalance.Denom {
			logger.Error("fibre migration: escrow denom differs from canonical record, leaving record untouched", "signer", signer, "canonical", canonical, "balance", account.Balance.String(), "canonical_balance", target.Balance.String())
			return
		}
		target.Balance = target.Balance.Add(account.Balance)
		target.AvailableBalance = target.AvailableBalance.Add(account.AvailableBalance)
	}

	withdrawals := k.GetWithdrawalsBySigner(ctx, signer)
	for _, withdrawal := range withdrawals {
		if withdrawal.Amount.Denom != account.Balance.Denom {
			logger.Error("fibre migration: withdrawal denom differs from escrow denom, leaving record untouched", "signer", signer, "amount", withdrawal.Amount.String(), "balance", account.Balance.String())
			return
		}
		if existing, exists := k.GetWithdrawal(ctx, canonical, withdrawal.RequestedTimestamp); exists && existing.Amount.Denom != withdrawal.Amount.Denom {
			logger.Error("fibre migration: withdrawal denom differs from canonical withdrawal, leaving record untouched", "signer", signer, "amount", withdrawal.Amount.String(), "canonical_amount", existing.Amount.String())
			return
		}
	}

	k.SetEscrowAccount(ctx, target)
	k.DeleteEscrowAccount(ctx, signer)

	merged := 0
	for _, withdrawal := range withdrawals {
		k.DeleteWithdrawal(ctx, withdrawal)
		if existing, exists := k.GetWithdrawal(ctx, canonical, withdrawal.RequestedTimestamp); exists {
			existing.Amount = existing.Amount.Add(withdrawal.Amount)
			k.SetWithdrawal(ctx, existing)
			merged++
			continue
		}
		withdrawal.Signer = canonical
		k.SetWithdrawal(ctx, withdrawal)
	}
	logger.Info("fibre migration: merged non-canonical escrow account into canonical record", "signer", signer, "canonical", canonical, "balance", account.Balance.String(), "available_balance", account.AvailableBalance.String(), "withdrawals_rekeyed", len(withdrawals)-merged, "withdrawals_merged", merged)
}

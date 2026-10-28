package keeper

import (
	"bytes"
	"fmt"

	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Migrator handles fibre module store migrations.
type Migrator struct {
	keeper Keeper
}

func NewMigrator(keeper Keeper) Migrator {
	return Migrator{keeper: keeper}
}

// Migrate1to2 makes v10 escrows accessible under v11's canonical signer keys
// and utia-only messages. Unsupported denominations are refunded in full;
// pending withdrawals are part of Balance, so they must not be paid separately.
func (m Migrator) Migrate1to2(ctx sdk.Context) error {
	ctx, write := ctx.CacheContext()
	k := m.keeper
	accounts, withdrawals, err := m.legacyEscrows(ctx)
	if err != nil {
		return err
	}

	// Remove the old keys before rebuilding: a canonical key may already exist
	// for the same owner. All iterators have been closed before any writes.
	for _, account := range accounts {
		k.DeleteEscrowAccount(ctx, account.Signer)
	}
	for _, withdrawal := range withdrawals {
		k.DeleteWithdrawal(ctx, withdrawal)
	}
	for _, account := range accounts {
		addr, err := sdk.AccAddressFromBech32(account.Signer)
		if err != nil {
			return err
		}
		if account.Balance.Denom != appconsts.BondDenom {
			if account.Balance.IsPositive() {
				if err := k.bankKeeper.SendCoinsFromModuleToAccount(ctx, types.ModuleName, addr, sdk.NewCoins(account.Balance)); err != nil {
					return fmt.Errorf("refund fibre escrow %s: %w", account.Signer, err)
				}
			}
			continue
		}
		account.Signer = addr.String()
		if existing, found := k.GetEscrowAccount(ctx, account.Signer); found {
			account.Balance.Amount, err = existing.Balance.Amount.SafeAdd(account.Balance.Amount)
			if err != nil {
				return fmt.Errorf("merge fibre escrow balance: %w", err)
			}
			account.AvailableBalance.Amount, err = existing.AvailableBalance.Amount.SafeAdd(account.AvailableBalance.Amount)
			if err != nil {
				return fmt.Errorf("merge fibre available balance: %w", err)
			}
		}
		k.SetEscrowAccount(ctx, account)
	}
	for _, withdrawal := range withdrawals {
		if withdrawal.Amount.Denom != appconsts.BondDenom {
			continue // Refunded with the owning escrow's total balance above.
		}
		addr, err := sdk.AccAddressFromBech32(withdrawal.Signer)
		if err != nil {
			return err
		}
		withdrawal.Signer = addr.String()
		if existing, found := k.GetWithdrawal(ctx, withdrawal.Signer, withdrawal.RequestedTimestamp); found {
			withdrawal.Amount.Amount, err = existing.Amount.Amount.SafeAdd(withdrawal.Amount.Amount)
			if err != nil {
				return fmt.Errorf("merge fibre withdrawal amount: %w", err)
			}
			// Aliases can have requests at the same block time with different
			// delays. Keep the later deadline so neither request pays early.
			if existing.AvailableTimestamp.After(withdrawal.AvailableTimestamp) {
				withdrawal.AvailableTimestamp = existing.AvailableTimestamp
			}
			k.DeleteWithdrawal(ctx, existing)
		}
		k.SetWithdrawal(ctx, withdrawal)
	}
	write()
	return nil
}

// legacyEscrows snapshots the two primary indexes in store order. Validate
// legacy state before coin arithmetic, and never mutate an open iterator's range.
func (m Migrator) legacyEscrows(ctx sdk.Context) ([]types.EscrowAccount, []types.Withdrawal, error) {
	store := ctx.KVStore(m.keeper.storeKey)
	var accounts []types.EscrowAccount
	var withdrawals []types.Withdrawal
	iterator := store.Iterator(types.EscrowAccountKeyPrefix, storetypes.PrefixEndBytes(types.WithdrawalsBySignerKeyPrefix))
	defer iterator.Close()
	for ; iterator.Valid(); iterator.Next() {
		switch {
		case bytes.HasPrefix(iterator.Key(), types.EscrowAccountKeyPrefix):
			var account types.EscrowAccount
			if err := m.keeper.cdc.Unmarshal(iterator.Value(), &account); err != nil {
				return nil, nil, fmt.Errorf("decode legacy fibre escrow: %w", err)
			}
			if _, err := sdk.AccAddressFromBech32(account.Signer); err != nil {
				return nil, nil, fmt.Errorf("invalid legacy fibre escrow signer: %w", err)
			}
			if !bytes.Equal(iterator.Key(), types.EscrowAccountKey(account.Signer)) ||
				!account.Balance.IsValid() || !account.AvailableBalance.IsValid() ||
				account.Balance.Denom != account.AvailableBalance.Denom ||
				account.AvailableBalance.Amount.GT(account.Balance.Amount) {
				return nil, nil, fmt.Errorf("invalid legacy fibre escrow %s", account.Signer)
			}
			accounts = append(accounts, account)
		case bytes.HasPrefix(iterator.Key(), types.WithdrawalsBySignerKeyPrefix):
			var withdrawal types.Withdrawal
			if err := m.keeper.cdc.Unmarshal(iterator.Value(), &withdrawal); err != nil {
				return nil, nil, fmt.Errorf("decode legacy fibre withdrawal: %w", err)
			}
			if _, err := sdk.AccAddressFromBech32(withdrawal.Signer); err != nil {
				return nil, nil, fmt.Errorf("invalid legacy fibre withdrawal signer: %w", err)
			}
			if !withdrawal.Amount.IsValid() || !withdrawal.Amount.IsPositive() ||
				withdrawal.RequestedTimestamp.IsZero() || withdrawal.AvailableTimestamp.Before(withdrawal.RequestedTimestamp) ||
				!bytes.Equal(iterator.Key(), types.WithdrawalsBySignerKey(withdrawal.Signer, withdrawal.RequestedTimestamp)) {
				return nil, nil, fmt.Errorf("invalid legacy fibre withdrawal for %s", withdrawal.Signer)
			}
			// Withdrawals must belong to an escrow of the same denomination;
			// otherwise dropping a non-utia request could strand reserved utia.
			var account types.EscrowAccount
			if err := m.keeper.cdc.Unmarshal(store.Get(types.EscrowAccountKey(withdrawal.Signer)), &account); err != nil {
				return nil, nil, fmt.Errorf("decode withdrawal's fibre escrow: %w", err)
			}
			if account.Balance.Denom != withdrawal.Amount.Denom {
				return nil, nil, fmt.Errorf("legacy fibre withdrawal denomination does not match escrow for %s", withdrawal.Signer)
			}
			withdrawals = append(withdrawals, withdrawal)
		}
	}
	return accounts, withdrawals, nil
}

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
	// The upgrade runs in the block's cached context. On error the caller must
	// discard that context; another full-state cache here doubles memory use.
	if err := m.validateLegacyEscrows(ctx); err != nil {
		return err
	}
	k := m.keeper
	// Remove unsupported records before canonicalizing: a lowercase key may
	// contain an unsupported denomination while its uppercase alias holds utia.
	for _, normalize := range []bool{false, true} {
		err := m.walkLegacyRecords(ctx, types.WithdrawalsBySignerKeyPrefix, storetypes.PrefixEndBytes(types.WithdrawalsBySignerKeyPrefix), func(_, value []byte) error {
			var withdrawal types.Withdrawal
			if err := k.cdc.Unmarshal(value, &withdrawal); err != nil {
				return err
			}
			if withdrawal.Amount.Denom != appconsts.BondDenom {
				if !normalize {
					k.DeleteWithdrawal(ctx, withdrawal)
				}
				return nil // Refunded with the owning escrow's total balance.
			}
			if !normalize {
				return nil
			}
			addr, err := sdk.AccAddressFromBech32(withdrawal.Signer)
			if err != nil {
				return err
			}
			if withdrawal.Signer == addr.String() {
				return nil
			}
			k.DeleteWithdrawal(ctx, withdrawal)
			withdrawal.Signer = addr.String()
			if existing, found := k.GetWithdrawal(ctx, withdrawal.Signer, withdrawal.RequestedTimestamp); found {
				withdrawal.Amount.Amount, err = existing.Amount.Amount.SafeAdd(withdrawal.Amount.Amount)
				if err != nil {
					return fmt.Errorf("merge fibre withdrawal amount: %w", err)
				}
				// Neither request may become available early after a merge.
				if existing.AvailableTimestamp.After(withdrawal.AvailableTimestamp) {
					withdrawal.AvailableTimestamp = existing.AvailableTimestamp
				}
				k.DeleteWithdrawal(ctx, existing)
			}
			k.SetWithdrawal(ctx, withdrawal)
			return nil
		})
		if err != nil {
			return err
		}
	}
	for _, normalize := range []bool{false, true} {
		err := m.walkLegacyRecords(ctx, types.EscrowAccountKeyPrefix, storetypes.PrefixEndBytes(types.EscrowAccountKeyPrefix), func(_, value []byte) error {
			var account types.EscrowAccount
			if err := k.cdc.Unmarshal(value, &account); err != nil {
				return err
			}
			if account.Balance.Denom == appconsts.BondDenom && !normalize {
				return nil
			}
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
				k.DeleteEscrowAccount(ctx, account.Signer)
				return nil
			}
			if account.Signer == addr.String() {
				return nil
			}
			k.DeleteEscrowAccount(ctx, account.Signer)
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
			return nil
		})
		if err != nil {
			return err
		}
	}
	return nil
}

// walkLegacyRecords closes each iterator before processing its fixed-size batch.
// Resume after the last original key even when the visitor deletes or moves it.
func (m Migrator) walkLegacyRecords(ctx sdk.Context, start, end []byte, visit func(key, value []byte) error) error {
	const batchSize = 128
	type record struct{ key, value []byte }
	store := ctx.KVStore(m.keeper.storeKey)
	for {
		batch := make([]record, 0, batchSize)
		iterator := store.Iterator(start, end)
		for ; iterator.Valid() && len(batch) < batchSize; iterator.Next() {
			batch = append(batch, record{bytes.Clone(iterator.Key()), bytes.Clone(iterator.Value())})
		}
		// SDK cache iterators report Error at normal exhaustion; Close is
		// the error boundary used by store traversal throughout the keeper.
		if err := iterator.Close(); err != nil {
			return err
		}
		if len(batch) == 0 {
			return nil
		}
		for _, entry := range batch {
			if err := visit(entry.key, entry.value); err != nil {
				return err
			}
		}
		start = batch[len(batch)-1].key
		start = append(start, 0)
	}
}

// validateLegacyEscrows checks primary indexes before making any changes.
func (m Migrator) validateLegacyEscrows(ctx sdk.Context) error {
	store := ctx.KVStore(m.keeper.storeKey)
	return m.walkLegacyRecords(ctx, types.EscrowAccountKeyPrefix, storetypes.PrefixEndBytes(types.WithdrawalsBySignerKeyPrefix), func(key, value []byte) error {
		switch {
		case bytes.HasPrefix(key, types.EscrowAccountKeyPrefix):
			var account types.EscrowAccount
			if err := m.keeper.cdc.Unmarshal(value, &account); err != nil {
				return fmt.Errorf("decode legacy fibre escrow: %w", err)
			}
			if _, err := sdk.AccAddressFromBech32(account.Signer); err != nil {
				return fmt.Errorf("invalid legacy fibre escrow signer: %w", err)
			}
			if !bytes.Equal(key, types.EscrowAccountKey(account.Signer)) ||
				!account.Balance.IsValid() || !account.AvailableBalance.IsValid() ||
				account.Balance.Denom != account.AvailableBalance.Denom ||
				account.AvailableBalance.Amount.GT(account.Balance.Amount) {
				return fmt.Errorf("invalid legacy fibre escrow %s", account.Signer)
			}
		case bytes.HasPrefix(key, types.WithdrawalsBySignerKeyPrefix):
			var withdrawal types.Withdrawal
			if err := m.keeper.cdc.Unmarshal(value, &withdrawal); err != nil {
				return fmt.Errorf("decode legacy fibre withdrawal: %w", err)
			}
			if _, err := sdk.AccAddressFromBech32(withdrawal.Signer); err != nil {
				return fmt.Errorf("invalid legacy fibre withdrawal signer: %w", err)
			}
			if !withdrawal.Amount.IsValid() || !withdrawal.Amount.IsPositive() ||
				withdrawal.RequestedTimestamp.IsZero() || withdrawal.AvailableTimestamp.Before(withdrawal.RequestedTimestamp) ||
				!bytes.Equal(key, types.WithdrawalsBySignerKey(withdrawal.Signer, withdrawal.RequestedTimestamp)) {
				return fmt.Errorf("invalid legacy fibre withdrawal for %s", withdrawal.Signer)
			}
			// Withdrawals must belong to an escrow of the same denomination;
			// otherwise dropping a non-utia request could strand reserved utia.
			var account types.EscrowAccount
			// A point iterator avoids populating the SDK read cache with every
			// unchanged canonical account referenced by a withdrawal.
			accountKey := types.EscrowAccountKey(withdrawal.Signer)
			iterator := store.Iterator(accountKey, append(bytes.Clone(accountKey), 0))
			var accountBytes []byte
			if iterator.Valid() {
				accountBytes = bytes.Clone(iterator.Value())
			}
			if err := iterator.Close(); err != nil {
				return err
			}
			if err := m.keeper.cdc.Unmarshal(accountBytes, &account); err != nil {
				return fmt.Errorf("decode withdrawal's fibre escrow: %w", err)
			}
			if account.Balance.Denom != withdrawal.Amount.Denom {
				return fmt.Errorf("legacy fibre withdrawal denomination does not match escrow for %s", withdrawal.Signer)
			}
		}
		return nil
	})
}

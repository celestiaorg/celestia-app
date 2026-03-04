package keeper

import (
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// InitGenesis initializes the module's state from a provided genesis state.
func (k Keeper) InitGenesis(ctx sdk.Context, genesisState types.GenesisState) {
	k.SetParams(ctx, genesisState.Params)

	for _, escrowAccount := range genesisState.EscrowAccounts {
		k.SetEscrowAccount(ctx, escrowAccount)
	}

	for _, withdrawal := range genesisState.Withdrawals {
		k.SetWithdrawal(ctx, withdrawal)
	}

	for _, entry := range genesisState.ProcessedPayments {
		k.SetProcessedPayment(ctx, entry)
	}
}

// ExportGenesis returns the module's exported genesis
func (k Keeper) ExportGenesis(ctx sdk.Context) *types.GenesisState {
	genesis := &types.GenesisState{}
	genesis.Params = k.GetParams(ctx)

	k.IterateEscrowAccounts(ctx, func(account types.EscrowAccount) bool {
		genesis.EscrowAccounts = append(genesis.EscrowAccounts, account)
		return false
	})

	k.IterateWithdrawals(ctx, func(withdrawal types.Withdrawal) bool {
		genesis.Withdrawals = append(genesis.Withdrawals, withdrawal)
		return false
	})

	k.IterateProcessedPayments(ctx, func(entry types.ProcessedPayment) bool {
		genesis.ProcessedPayments = append(genesis.ProcessedPayments, entry)
		return false
	})

	return genesis
}

// IterateEscrowAccounts iterates over all escrow accounts and calls the provided callback function
func (k Keeper) IterateEscrowAccounts(ctx sdk.Context, callback func(account types.EscrowAccount) bool) {
	store := ctx.KVStore(k.storeKey)
	iterator := storetypes.KVStorePrefixIterator(store, types.EscrowAccountKeyPrefix)
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var account types.EscrowAccount
		k.cdc.MustUnmarshal(iterator.Value(), &account)
		if callback(account) {
			break
		}
	}
}

// IterateWithdrawals iterates over all withdrawals and calls the provided callback function
func (k Keeper) IterateWithdrawals(ctx sdk.Context, callback func(withdrawal types.Withdrawal) bool) {
	store := ctx.KVStore(k.storeKey)
	iterator := storetypes.KVStorePrefixIterator(store, types.WithdrawalsBySignerKeyPrefix)
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var withdrawal types.Withdrawal
		k.cdc.MustUnmarshal(iterator.Value(), &withdrawal)
		if callback(withdrawal) {
			break
		}
	}
}

// IterateProcessedPayments iterates over all processed payments and calls the provided callback function
func (k Keeper) IterateProcessedPayments(ctx sdk.Context, callback func(entry types.ProcessedPayment) bool) {
	store := ctx.KVStore(k.storeKey)
	iterator := storetypes.KVStorePrefixIterator(store, types.ProcessedPaymentsByHashKeyPrefix)
	defer iterator.Close()

	for ; iterator.Valid(); iterator.Next() {
		var entry types.ProcessedPayment
		k.cdc.MustUnmarshal(iterator.Value(), &entry)
		if callback(entry) {
			break
		}
	}
}

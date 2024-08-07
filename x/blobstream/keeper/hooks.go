package keeper

import (
	"cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v3/x/blobstream/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Hooks is a wrapper struct around Keeper.
type Hooks struct {
	k Keeper
}

// Hooks Create new Blobstream hooks
func (k Keeper) Hooks() Hooks {
	// if startup is mis-ordered in app.go this hook will halt the chain when
	// called. Keep this check to make such a mistake obvious
	if k.storeKey == nil {
		panic("hooks initialized before BlobstreamKeeper")
	}
	return Hooks{k}
}

func (h Hooks) AfterValidatorBeginUnbonding(ctx sdk.Context, _ sdk.ConsAddress, _ sdk.ValAddress) error {
	if ctx.BlockHeader().Version.App > 1 {
		// no-op if the app version is greater than 1 because blobstream was disabled in v2.
		return nil
	}
	// When Validator starts Unbonding, Persist the block height in the store.
	// Later in EndBlocker, check if there is at least one validator who started
	// unbonding and create a valset request. The reason for creating valset
	// requests in EndBlock is to create only one valset request per block if
	// multiple validators start unbonding in the same block.

	// This hook is called for jailing or unbonding triggered by users but it is
	// NOT called for jailing triggered in the endblocker therefore we call the
	// keeper function ourselves there.
	h.k.SetLatestUnBondingBlockHeight(ctx, uint64(ctx.BlockHeight()))
	return nil
}

func (h Hooks) BeforeDelegationCreated(_ sdk.Context, _ sdk.AccAddress, _ sdk.ValAddress) error {
	return nil
}

func (h Hooks) AfterValidatorCreated(ctx sdk.Context, addr sdk.ValAddress) error {
	if ctx.BlockHeader().Version.App > 1 {
		// no-op if the app version is greater than 1 because blobstream was disabled in v2.
		return nil
	}
	defaultEvmAddr := types.DefaultEVMAddress(addr)
	// This should practically never happen that we have a collision. It may be
	// bad UX to reject the attempt to create a validator and require the user to
	// generate a new set of keys but this ensures EVM address uniqueness
	if !h.k.IsEVMAddressUnique(ctx, defaultEvmAddr) {
		return errors.Wrapf(types.ErrEVMAddressAlreadyExists, "create a validator with a different operator address to %s (pubkey collision)", addr.String())
	}
	h.k.SetEVMAddress(ctx, addr, defaultEvmAddr)
	return nil
}

func (h Hooks) BeforeValidatorModified(_ sdk.Context, _ sdk.ValAddress) error {
	return nil
}

func (h Hooks) AfterValidatorBonded(_ sdk.Context, _ sdk.ConsAddress, _ sdk.ValAddress) error {
	return nil
}

func (h Hooks) BeforeDelegationRemoved(_ sdk.Context, _ sdk.AccAddress, _ sdk.ValAddress) error {
	return nil
}

func (h Hooks) AfterValidatorRemoved(_ sdk.Context, _ sdk.ConsAddress, _ sdk.ValAddress) error {
	return nil
}

func (h Hooks) BeforeValidatorSlashed(_ sdk.Context, _ sdk.ValAddress, _ sdk.Dec) error {
	return nil
}

func (h Hooks) BeforeDelegationSharesModified(_ sdk.Context, _ sdk.AccAddress, _ sdk.ValAddress) error {
	return nil
}

func (h Hooks) AfterDelegationModified(_ sdk.Context, _ sdk.AccAddress, _ sdk.ValAddress) error {
	return nil
}

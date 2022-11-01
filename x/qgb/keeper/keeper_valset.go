package keeper

import (
	"fmt"
	"math/big"

	cosmosmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetLatestValset returns the latest validator set in store. This is different
// from the CurrentValset because this one has been saved and is therefore *the* latest valset
// saved in store. GetCurrentValset shows you what could be, if you chose to save it, this function
// shows you what is the latest valset that was saved.
// Panics if no valset is found. Because, a valset is always created when starting
// the chain. Check x/qgb/abci.go:68 for more information.
func (k Keeper) GetLatestValset(ctx sdk.Context) (*types.Valset, error) {
	nonce := k.GetLatestAttestationNonce(ctx)
	for i := uint64(0); i <= nonce; i++ {
		at, found, err := k.GetAttestationByNonce(ctx, nonce-i)
		if err != nil {
			return nil, err
		}
		if !found {
			panic(sdkerrors.Wrap(
				types.ErrNilAttestation,
				fmt.Sprintf("stumbled upon nil attestation for nonce %d", i),
			))
		}
		if at.Type() == types.ValsetRequestType {
			valset, ok := at.(*types.Valset)
			if !ok {
				return nil, sdkerrors.Wrap(types.ErrAttestationNotValsetRequest, "couldn't cast attestation to valset")
			}
			return valset, nil
		}
	}
	// should never execute
	panic(sdkerrors.Wrap(sdkerrors.ErrNotFound, "couldn't find latest valset"))
}

// SetLastUnBondingBlockHeight sets the last unbonding block height. Note this
// value is not saved to state or loaded at genesis. This value is reset to zero
// on chain upgrade.
func (k Keeper) SetLastUnBondingBlockHeight(ctx sdk.Context, unbondingBlockHeight uint64) {
	store := ctx.KVStore(k.storeKey)
	store.Set([]byte(types.LastUnBondingBlockHeight), types.UInt64Bytes(unbondingBlockHeight))
}

// GetLastUnBondingBlockHeight returns the last unbonding block height or zero
// if not set. This value is not saved or loaded at genesis. This value is reset
// to zero on chain upgrade.
func (k Keeper) GetLastUnBondingBlockHeight(ctx sdk.Context) uint64 {
	store := ctx.KVStore(k.storeKey)
	bytes := store.Get([]byte(types.LastUnBondingBlockHeight))

	if len(bytes) == 0 {
		return 0
	}
	return UInt64FromBytes(bytes)
}

func (k Keeper) GetCurrentValset(ctx sdk.Context) (types.Valset, error) {
	validators := k.StakingKeeper.GetBondedValidatorsByPower(ctx)
	if len(validators) == 0 {
		return types.Valset{}, types.ErrNoValidators
	}
	// allocate enough space for all validators, but len zero, we then append
	// so that we have an array with extra capacity but the correct length depending
	// on how many validators have keys set.
	bridgeValidators := make([]*types.InternalBridgeValidator, 0, len(validators))
	totalPower := sdk.NewInt(0)
	// TODO someone with in depth info on Cosmos staking should determine
	// if this is doing what I think it's doing
	for _, validator := range validators {
		val := validator.GetOperator()
		if err := sdk.VerifyAddressFormat(val); err != nil {
			return types.Valset{}, sdkerrors.Wrap(err, types.ErrInvalidValAddress.Error())
		}

		p := sdk.NewInt(k.StakingKeeper.GetLastValidatorPower(ctx, val))

		// TODO make sure this  is always the case
		bv := types.BridgeValidator{Power: p.Uint64(), EvmAddress: validator.EvmAddress}
		ibv, err := types.NewInternalBridgeValidator(bv)
		if err != nil {
			return types.Valset{}, sdkerrors.Wrapf(err, types.ErrInvalidEVMAddress.Error(), val)
		}
		bridgeValidators = append(bridgeValidators, ibv)
		totalPower = totalPower.Add(p)
	}
	// normalize power values to the maximum bridge power which is 2^32
	for i := range bridgeValidators {
		bridgeValidators[i].Power = normalizeValidatorPower(bridgeValidators[i].Power, totalPower)
	}

	// increment the nonce, since this potential future valset should be after the current valset
	valsetNonce := k.GetLatestAttestationNonce(ctx) + 1

	valset, err := types.NewValset(valsetNonce, uint64(ctx.BlockHeight()), bridgeValidators)
	if err != nil {
		return types.Valset{}, (sdkerrors.Wrap(err, types.ErrInvalidValset.Error()))
	}
	return *valset, nil
}

// normalizeValidatorPower scales rawPower with respect to totalValidatorPower to take a value between 0 and 2^32
// Uses BigInt operations to avoid overflow errors
// Example: rawPower = max (2^63 - 1), totalValidatorPower = 1 validator: (2^63 - 1)
//
//	result: (2^63 - 1) * 2^32 / (2^63 - 1) = 2^32 = 4294967296 [this is the multiplier value below, our max output]
//
// Example: rawPower = max (2^63 - 1), totalValidatorPower = 1000 validators with the same power: 1000*(2^63 - 1)
//
//	result: (2^63 - 1) * 2^32 / (1000(2^63 - 1)) = 2^32 / 1000 = 4294967
func normalizeValidatorPower(rawPower uint64, totalValidatorPower cosmosmath.Int) uint64 {
	// Compute rawPower * multiplier / quotient
	// Set the upper limit to 2^32, which would happen if there is a single validator with all the power
	multiplier := new(big.Int).SetUint64(4294967296)
	// Scale by current validator powers, a particularly low-power validator (1 out of over 2^32) would have 0 power
	quotient := new(big.Int).Set(totalValidatorPower.BigInt())
	power := new(big.Int).SetUint64(rawPower)
	power.Mul(power, multiplier)
	power.Quo(power, quotient)
	return power.Uint64()
}

// GetLastValsetBeforeNonce returns the previous valset before the provided `nonce`.
// the `nonce` can be a valset, but this method will return the valset before it.
// If the provided nonce is 1. It will return an error. Because, there is no valset before nonce 1.
func (k Keeper) GetLastValsetBeforeNonce(ctx sdk.Context, nonce uint64) (*types.Valset, error) {
	if nonce == 1 {
		return nil, types.ErrNoValsetBeforeNonceOne
	}
	if nonce > k.GetLatestAttestationNonce(ctx) {
		return nil, types.ErrNonceHigherThanLatestAttestationNonce
	}
	// starting at 1 because the current nonce can be a valset
	// and we need the previous one.
	for i := uint64(1); i < nonce; i++ {
		at, found, err := k.GetAttestationByNonce(ctx, nonce-i)
		if err != nil {
			return nil, err
		}
		if !found {
			return nil, sdkerrors.Wrap(
				types.ErrNilAttestation,
				fmt.Sprintf("nonce=%d", nonce-i),
			)
		}
		if at.Type() == types.ValsetRequestType {
			valset, ok := at.(*types.Valset)
			if !ok {
				return nil, sdkerrors.Wrap(types.ErrAttestationNotValsetRequest, "couldn't cast attestation to valset")
			}
			return valset, nil
		}
	}
	return nil, sdkerrors.Wrap(
		sdkerrors.ErrNotFound,
		fmt.Sprintf("couldn't find valset before nonce %d", nonce),
	)
}

// TODO add query for this method and make the orchestrator Querier use it.
// GetValsetByNonce returns the stored valset associated with the provided nonce.
// Returns (nil, false, nil) if not found.
func (k Keeper) GetValsetByNonce(ctx sdk.Context, nonce uint64) (*types.Valset, bool, error) {
	at, found, err := k.GetAttestationByNonce(ctx, nonce)
	if err != nil {
		return nil, false, err
	}
	if !found {
		return nil, false, nil
	}
	if at.Type() != types.ValsetRequestType {
		return nil, false, sdkerrors.Wrap(types.ErrAttestationNotValsetRequest, "attestation is not a valset request")
	}

	valset, ok := at.(*types.Valset)
	if !ok {
		return nil, false, sdkerrors.Wrap(types.ErrAttestationNotValsetRequest, "couldn't cast attestation to valset")
	}
	return valset, true, nil
}

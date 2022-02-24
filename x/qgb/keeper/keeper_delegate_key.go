package keeper

import (
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"sort"
)

// GetDelegateKeys iterates both the EthAddress and Orchestrator address indexes to produce
// a vector of MsgSetOrchestratorAddress entries containing all the delegate keys for state
// export / import.
func (k Keeper) GetDelegateKeys(ctx sdk.Context) []types.MsgSetOrchestratorAddress {
	store := ctx.KVStore(k.storeKey)
	prefix := []byte(types.EthAddressByValidatorKey)
	iter := store.Iterator(prefixRange(prefix))
	defer iter.Close()

	ethAddresses := make(map[string]string)

	for ; iter.Valid(); iter.Next() {
		// the 'key' contains both the prefix and the value, so we need
		// to cut off the starting bytes, if you don't do this a valid
		// cosmos key will be made out of EthAddressByValidatorKey + the starting bytes
		// of the actual key
		key := iter.Key()[len(types.EthAddressByValidatorKey):]
		value := iter.Value()
		ethAddress, err := types.NewEthAddress(string(value))
		if err != nil {
			panic(sdkerrors.Wrapf(err, "found invalid ethAddress %v under key %v", string(value), key))
		}
		valAddress := sdk.ValAddress(key)
		if err := sdk.VerifyAddressFormat(valAddress); err != nil {
			panic(sdkerrors.Wrapf(err, "invalid valAddress in key %v", valAddress))
		}
		ethAddresses[valAddress.String()] = ethAddress.GetAddress()
	}

	store = ctx.KVStore(k.storeKey)
	prefix = []byte(types.KeyOrchestratorAddress)
	iter = store.Iterator(prefixRange(prefix))
	defer iter.Close()

	orchAddresses := make(map[string]string)

	for ; iter.Valid(); iter.Next() {
		key := iter.Key()[len(types.KeyOrchestratorAddress):]
		value := iter.Value()
		orchAddress := sdk.AccAddress(key)
		if err := sdk.VerifyAddressFormat(orchAddress); err != nil {
			panic(sdkerrors.Wrapf(err, "invalid orchAddress in key %v", orchAddresses))
		}
		valAddress := sdk.ValAddress(value)
		if err := sdk.VerifyAddressFormat(valAddress); err != nil {
			panic(sdkerrors.Wrapf(err, "invalid val address stored for orchestrator %s", valAddress.String()))
		}

		orchAddresses[valAddress.String()] = orchAddress.String()
	}

	var result []types.MsgSetOrchestratorAddress

	for valAddr, ethAddr := range ethAddresses {
		orch, ok := orchAddresses[valAddr]
		if !ok {
			// this should never happen unless the store
			// is somehow inconsistent
			panic("Can't find address")
		}
		result = append(result, types.MsgSetOrchestratorAddress{
			Orchestrator: orch,
			Validator:    valAddr,
			EthAddress:   ethAddr,
		})

	}

	// we iterated over a map, so now we have to sort to ensure the
	// output here is deterministic, eth address chosen for no particular
	// reason
	sort.Slice(result[:], func(i, j int) bool {
		return result[i].EthAddress < result[j].EthAddress
	})

	return result
}

// GetEthAddressByValidator returns the eth address for a given qgb validator
func (k Keeper) GetEthAddressByValidator(ctx sdk.Context, validator sdk.ValAddress) (ethAddress *types.EthAddress, found bool) {
	if err := sdk.VerifyAddressFormat(validator); err != nil {
		panic(sdkerrors.Wrap(err, "invalid validator address"))
	}
	store := ctx.KVStore(k.storeKey)
	ethAddr := store.Get([]byte(types.GetEthAddressByValidatorKey(validator)))
	if ethAddr == nil {
		return nil, false
	}

	addr, err := types.NewEthAddress(string(ethAddr))
	if err != nil {
		return nil, false
	}
	return addr, true
}

// SetOrchestratorValidator sets the Orchestrator key for a given validator
func (k Keeper) SetOrchestratorValidator(ctx sdk.Context, val sdk.ValAddress, orch sdk.AccAddress) {
	if err := sdk.VerifyAddressFormat(val); err != nil {
		panic(sdkerrors.Wrap(err, "invalid val address"))
	}
	if err := sdk.VerifyAddressFormat(orch); err != nil {
		panic(sdkerrors.Wrap(err, "invalid orch address"))
	}
	store := ctx.KVStore(k.storeKey)
	store.Set([]byte(types.GetOrchestratorAddressKey(orch)), val.Bytes())
}

// SetEthAddressForValidator sets the ethereum address for a given validator
func (k Keeper) SetEthAddressForValidator(ctx sdk.Context, validator sdk.ValAddress, ethAddr types.EthAddress) {
	if err := sdk.VerifyAddressFormat(validator); err != nil {
		panic(sdkerrors.Wrap(err, "invalid validator address"))
	}
	store := ctx.KVStore(k.storeKey)
	store.Set([]byte(types.GetEthAddressByValidatorKey(validator)), []byte(ethAddr.GetAddress()))
	store.Set([]byte(types.GetValidatorByEthAddressKey(ethAddr)), validator)
}

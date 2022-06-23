package keeper

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SetAttestationRequest Sets a new attestation request to the store to be signed
// by orchestrators afterwards.
func (k Keeper) SetAttestationRequest(ctx sdk.Context, at types.AttestationRequestI) error {
	k.StoreAttestation(ctx, at)
	k.SetLatestAttestationNonce(ctx, at.GetNonce())

	ctx.EventManager().EmitEvent(
		sdk.NewEvent(
			types.EventTypeAttestationRequest,
			sdk.NewAttribute(sdk.AttributeKeyModule, types.ModuleName),
			sdk.NewAttribute(types.AttributeKeyNonce, fmt.Sprint(at.GetNonce())),
		),
	)
	return nil
}

// StoreAttestation
func (k Keeper) StoreAttestation(ctx sdk.Context, at types.AttestationRequestI) {
	nonce := at.GetNonce()
	key := []byte(types.GetAttestationKey(nonce))
	store := ctx.KVStore(k.storeKey)

	if store.Has(key) {
		panic("Trying to overwrite existing attestation request!")
	}

	b, err := k.cdc.MarshalInterface(at)
	if err != nil {
		panic(err)
	}
	store.Set((key), b)
}

// SetLatestAttestationNonce sets the latest attestation request nonce, since it's
// expected that this value will only increase it panics on an attempt
// to decrement
func (k Keeper) SetLatestAttestationNonce(ctx sdk.Context, nonce uint64) {
	// TODO add test
	// this is purely an increasing counter and should never decrease
	if k.CheckLatestAttestationNonce(ctx) && k.GetLatestAttestationNonce(ctx) > nonce {
		panic("Decrementing attestation nonce!")
	}

	store := ctx.KVStore(k.storeKey)
	store.Set([]byte(types.LatestAttestationtNonce), types.UInt64Bytes(nonce))
}

// CheckLatestAttestationNonce returns true if the latest attestation request nonce
// is declared in the store and false if it has not been initialized
func (k Keeper) CheckLatestAttestationNonce(ctx sdk.Context) bool {
	store := ctx.KVStore(k.storeKey)
	has := store.Has([]byte(types.LatestAttestationtNonce))
	return has
}

// GetLatestAttestationNonce returns the latest attestation request nonce
func (k Keeper) GetLatestAttestationNonce(ctx sdk.Context) uint64 {
	if ctx.BlockHeight() <= int64(1) { // temporarily to avoid concurrent map exception
		// TODO: handle this case for genesis properly. Note for Evan: write an issue
		return 0
	}

	store := ctx.KVStore(k.storeKey)
	bytes := store.Get([]byte(types.LatestAttestationtNonce))
	return UInt64FromBytes(bytes)
}

// GetAttestationByNonce returns an attestation request by nonce
func (k Keeper) GetAttestationByNonce(ctx sdk.Context, nonce uint64) types.AttestationRequestI {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get([]byte(types.GetAttestationKey(nonce)))
	if bz == nil {
		return nil
	}
	var at types.AttestationRequestI
	err := k.cdc.UnmarshalInterface(bz, &at)
	if err != nil {
		panic(err)
	}
	return at
}

package keeper

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v3/x/blobstream/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SetAttestationRequest sets a new attestation request to the store to be
// signed by orchestrators afterwards.
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

// StoreAttestation saves the attestation in store. Should panic if overwriting
// existing one.
func (k Keeper) StoreAttestation(ctx sdk.Context, at types.AttestationRequestI) {
	nonce := at.GetNonce()
	key := []byte(types.GetAttestationKey(nonce))
	store := ctx.KVStore(k.storeKey)

	if store.Has(key) {
		panic("trying to overwrite existing attestation request")
	}

	b, err := k.cdc.MarshalInterface(at)
	if err != nil {
		panic(err)
	}
	store.Set((key), b)
}

// SetLatestAttestationNonce sets the latest attestation request nonce, since
// it's expected that this value will only increase by one and it panics
// otherwise.
func (k Keeper) SetLatestAttestationNonce(ctx sdk.Context, nonce uint64) {
	// in case the latest attestation nonce doesn't exist, we proceed to
	// initialize it in the store. however, if it already exists, we check if
	// the nonce is correctly incremented.
	if k.CheckLatestAttestationNonce(ctx) && k.GetLatestAttestationNonce(ctx)+1 != nonce {
		panic("not incrementing latest attestation nonce correctly")
	}

	store := ctx.KVStore(k.storeKey)
	store.Set([]byte(types.LatestAttestationNonce), types.UInt64Bytes(nonce))
}

// CheckLatestAttestationNonce returns true if the latest attestation request
// nonce is declared in the store and false if it has not been initialized.
func (k Keeper) CheckLatestAttestationNonce(ctx sdk.Context) bool {
	fmt.Println(k.storeKey, "STORE KEY")
	store := ctx.KVStore(k.storeKey)
	has := store.Has([]byte(types.LatestAttestationNonce))
	return has
}

// GetLatestAttestationNonce returns the latest attestation request nonce.
// Panics if the latest attestation nonce doesn't exist in store. This value is
// set on chain startup. However, it won't be written to store until height = 1.
// To check if this value exists in store, use the `CheckLatestAttestationNonce`
// method.
func (k Keeper) GetLatestAttestationNonce(ctx sdk.Context) uint64 {
	store := ctx.KVStore(k.storeKey)
	fmt.Println("store key", k.storeKey)
	bytes := store.Get([]byte(types.LatestAttestationNonce))
	if bytes == nil {
		panic("nil LatestAttestationNonce")
	}
	return UInt64FromBytes(bytes)
}

// CheckEarliestAvailableAttestationNonce returns true if the earliest available
// attestation nonce has been initialized in store, and false if not.
func (k Keeper) CheckEarliestAvailableAttestationNonce(ctx sdk.Context) bool {
	store := ctx.KVStore(k.storeKey)
	has := store.Has([]byte(types.EarliestAvailableAttestationNonce))
	return has
}

// GetEarliestAvailableAttestationNonce returns the earliest available
// attestation nonce. The nonce is of the earliest available attestation in
// store that can be retrieved. Panics if the earliest available attestation
// nonce doesn't exist in store. This value is set on chain startup. However, it
// won't be written to store until height = 1. To check if this value exists in
// store, use the `CheckEarliestAvailableAttestationNonce` method.
func (k Keeper) GetEarliestAvailableAttestationNonce(ctx sdk.Context) uint64 {
	store := ctx.KVStore(k.storeKey)
	bytes := store.Get([]byte(types.EarliestAvailableAttestationNonce))
	if bytes == nil {
		panic("nil earliest available attestation nonce")
	}
	return UInt64FromBytes(bytes)
}

// SetEarliestAvailableAttestationNonce sets the earliest available attestation
// nonce. The nonce is of the earliest available attestation in store that can
// be retrieved.
func (k Keeper) SetEarliestAvailableAttestationNonce(ctx sdk.Context, nonce uint64) {
	store := ctx.KVStore(k.storeKey)
	store.Set([]byte(types.EarliestAvailableAttestationNonce), types.UInt64Bytes(nonce))
}

// GetAttestationByNonce returns an attestation request by nonce. Returns (nil,
// false, nil) if the attestation is not found.
func (k Keeper) GetAttestationByNonce(ctx sdk.Context, nonce uint64) (types.AttestationRequestI, bool, error) {
	store := ctx.KVStore(k.storeKey)
	bz := store.Get([]byte(types.GetAttestationKey(nonce)))
	if bz == nil {
		return nil, false, nil
	}
	var at types.AttestationRequestI
	err := k.cdc.UnmarshalInterface(bz, &at)
	if err != nil {
		return nil, false, types.ErrUnmarshalllAttestation
	}
	return at, true, nil
}

// DeleteAttestation deletes an attestation from state. Will do nothing if the
// attestation doesn't exist in store.
func (k Keeper) DeleteAttestation(ctx sdk.Context, nonce uint64) {
	key := []byte(types.GetAttestationKey(nonce))
	store := ctx.KVStore(k.storeKey)
	if !store.Has(key) {
		// if the store doesn't have the needed attestation, then no need to do
		// anything.
		return
	}
	store.Delete(key)
}

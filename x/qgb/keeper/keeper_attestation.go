package keeper

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// SetAttestationRequest sets a new attestation request to the store to be signed
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

// StoreAttestation saves the attestation in store.
// Should panic if overwriting existing one.
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

// SetLatestAttestationNonce sets the latest attestation request nonce, since it's
// expected that this value will only increase by one and it panics otherwise.
func (k Keeper) SetLatestAttestationNonce(ctx sdk.Context, nonce uint64) {
	if k.CheckLatestAttestationNonce(ctx) && k.GetLatestAttestationNonce(ctx)+1 != nonce {
		panic("not incrementing latest attestation nonce correctly")
	}

	store := ctx.KVStore(k.storeKey)
	store.Set([]byte(types.LatestAttestationtNonce), types.UInt64Bytes(nonce))
}

// CheckLatestAttestationNonce returns true if the latest attestation request nonce
// is declared in the store and false if it has not been initialized.
func (k Keeper) CheckLatestAttestationNonce(ctx sdk.Context) bool {
	store := ctx.KVStore(k.storeKey)
	has := store.Has([]byte(types.LatestAttestationtNonce))
	return has
}

// GetLatestAttestationNonce returns the latest attestation request nonce.
// Panics if the latest attestation nonce doesn't exit. Make sure to call `CheckLatestAttestationNonce`
// before getting the nonce.
// This value is set on chain startup, it shouldn't panic in normal conditions.
// Check x/qgb/genesis.go for more information.
func (k Keeper) GetLatestAttestationNonce(ctx sdk.Context) uint64 {
	store := ctx.KVStore(k.storeKey)
	bytes := store.Get([]byte(types.LatestAttestationtNonce))
	if bytes == nil {
		panic("nil LatestAttestationNonce")
	}
	return UInt64FromBytes(bytes)
}

// GetAttestationByNonce returns an attestation request by nonce.
// Returns (nil, false, nil) if the attestation is not found.
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

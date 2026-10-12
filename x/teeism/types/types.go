package types

import (
	"context"
	"errors"

	errorsmod "cosmossdk.io/errors"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	// ModuleTypeTeeISM defines the x/teeism module identifier.
	//
	// The identifier binds this module to the Hyperlane core ISM router and is
	// encoded directly into the HexAddresses of ISMs this module creates. 43 is
	// arbitrary; it only has to sit outside the range Hyperlane core reserves and
	// not collide with another local module, which is why it is one past the 42
	// x/zkism uses.
	ModuleTypeTeeISM uint8 = 43
)

var _ ismtypes.HyperlaneInterchainSecurityModule = (*InterchainSecurityModule)(nil)

// GetId implements types.HyperlaneInterchainSecurityModule.
func (ism *InterchainSecurityModule) GetId() (util.HexAddress, error) {
	if ism.Id.IsZeroAddress() {
		return util.HexAddress{}, errors.New("address is empty")
	}
	return ism.Id, nil
}

// ModuleType implements types.HyperlaneInterchainSecurityModule.
func (ism *InterchainSecurityModule) ModuleType() uint8 {
	return ModuleTypeTeeISM
}

// Verify implements types.HyperlaneInterchainSecurityModule.
//
// NOTE: This returns ErrNotSupported and exists only to satisfy the ISM
// interface. Verification runs through the x/teeism keeper entrypoint, which is
// what the ISM router calls; this method should never be reached.
func (ism *InterchainSecurityModule) Verify(ctx context.Context, metadata []byte, message util.HyperlaneMessage) (bool, error) {
	return false, sdkerrors.ErrNotSupported
}

// IdentityDigest returns the digest of the enclave this ISM pins.
func (ism *InterchainSecurityModule) IdentityDigest() ([32]byte, error) {
	if ism.Identity == nil {
		return [32]byte{}, ErrInvalidEnclaveIdentity.Wrap("ism has no pinned enclave identity")
	}
	return ism.Identity.Digest(), nil
}

// Repin points the ism at a different enclave on behalf of `owner`, and returns the
// identity digest it pinned before.
//
// Only the owner may, and only to an identity that could match a real quote. The
// trusted state keeps its root, height and light client store; the identity digest
// it carries is swapped too, because the enclave copies it forward into every state
// it attests and the next attestation is checked against the new pin.
func (ism *InterchainSecurityModule) Repin(owner string, identity *EnclaveIdentity) ([32]byte, error) {
	if err := ism.authorize(owner); err != nil {
		return [32]byte{}, err
	}
	if err := identity.Validate(); err != nil {
		return [32]byte{}, err
	}
	state, err := DecodeIsmState(ism.State)
	if err != nil {
		return [32]byte{}, errorsmod.Wrap(ErrInvalidTrustedState, err.Error())
	}
	previous := state.IdentityDigest
	state.IdentityDigest = identity.Digest()
	ism.State = EncodeIsmState(state)
	ism.Identity = identity
	return previous, nil
}

// authorize refuses anyone but the ism's owner. An ism without an owner refuses
// everyone.
func (ism *InterchainSecurityModule) authorize(owner string) error {
	if ism.Owner == "" || owner != ism.Owner {
		return errorsmod.Wrapf(sdkerrors.ErrUnauthorized, "%s does not own ism %s", owner, ism.Id.String())
	}
	return nil
}

// TransferOwnership hands the ism to `newOwner` on behalf of `owner`, and returns
// the owner before.
//
// There is no renouncing: an ism without an owner could never again be re-pinned
// or have its verifier overridden, which are the controls an emergency needs.
func (ism *InterchainSecurityModule) TransferOwnership(owner, newOwner string) (string, error) {
	if err := ism.authorize(owner); err != nil {
		return "", err
	}
	if newOwner == "" {
		return "", errorsmod.Wrap(sdkerrors.ErrInvalidAddress, "new owner must not be empty")
	}
	previous := ism.Owner
	ism.Owner = newOwner
	return previous, nil
}

// SetVerifierOverrides replaces the ism's overrides of Intel's side of
// verification on behalf of `owner`, and returns the overrides before. Empty
// overrides clear them.
func (ism *InterchainSecurityModule) SetVerifierOverrides(owner string, overrides *VerifierOverrides) (*VerifierOverrides, error) {
	if err := ism.authorize(owner); err != nil {
		return nil, err
	}
	if err := overrides.Validate(); err != nil {
		return nil, err
	}
	previous := ism.VerifierOverrides
	if overrides.IsEmpty() {
		ism.VerifierOverrides = nil
	} else {
		ism.VerifierOverrides = overrides
	}
	return previous, nil
}

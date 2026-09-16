package types

import (
	"context"
	"errors"

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

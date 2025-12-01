package types

import (
	"context"
	"errors"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	ModuleTypeZkISM = 42
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
	return ModuleTypeZkISM
}

// Verify implements types.HyperlaneInterchainSecurityModule.
// NOTE: The following method returns an ErrNotSupported error as this method is implemented primarily to satisfy the ISM interface.
// ISM verification is performed exclusively through the x/zkism keeper entrypoint. This method should never be called by integration points.
func (ism *InterchainSecurityModule) Verify(ctx context.Context, metadata []byte, message util.HyperlaneMessage) (bool, error) {
	return false, sdkerrors.ErrNotSupported
}

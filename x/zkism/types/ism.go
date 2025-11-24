package types

import (
	"context"
	"errors"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

const (
	InterchainSecurityModuleTypeZKExecution     = 42
	InterchainSecurityModuleTypeStateTransition = 43
)

var _ ismtypes.HyperlaneInterchainSecurityModule = (*EvolveEvmISM)(nil)
var _ ismtypes.HyperlaneInterchainSecurityModule = (*ConsensusISM)(nil)

// GetId implements types.HyperlaneInterchainSecurityModule.
func (ism *EvolveEvmISM) GetId() (util.HexAddress, error) {
	if ism.Id.IsZeroAddress() {
		return util.HexAddress{}, errors.New("address is empty")
	}

	return ism.Id, nil
}

// ModuleType implements types.HyperlaneInterchainSecurityModule.
func (ism *EvolveEvmISM) ModuleType() uint8 {
	return InterchainSecurityModuleTypeZKExecution
}

// Verify implements types.HyperlaneInterchainSecurityModule.
func (ism *EvolveEvmISM) Verify(ctx context.Context, metadata []byte, message util.HyperlaneMessage) (bool, error) {
	return false, sdkerrors.ErrNotSupported
}

// GetId implements types.HyperlaneInterchainSecurityModule.
func (v *ConsensusISM) GetId() (util.HexAddress, error) {
	if v.Id.IsZeroAddress() {
		return util.HexAddress{}, errors.New("address is empty")
	}

	return v.Id, nil
}

// ModuleType implements types.HyperlaneInterchainSecurityModule.
func (v *ConsensusISM) ModuleType() uint8 {
	return InterchainSecurityModuleTypeStateTransition
}

// Verify implements types.HyperlaneInterchainSecurityModule.
func (v *ConsensusISM) Verify(ctx context.Context, metadata []byte, message util.HyperlaneMessage) (bool, error) {
	return false, sdkerrors.ErrNotSupported
}

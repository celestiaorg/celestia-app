package types

import (
	"context"
	"errors"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
)

const (
	InterchainSecurityModuleTypeZKExecution = 42
)

var _ ismtypes.HyperlaneInterchainSecurityModule = (*ZKExecutionISM)(nil)

// GetId implements types.HyperlaneInterchainSecurityModule.
func (z *ZKExecutionISM) GetId() (util.HexAddress, error) {
	if z.Id.IsZeroAddress() {
		return util.HexAddress{}, errors.New("address is empty")
	}

	return z.Id, nil
}

// ModuleType implements types.HyperlaneInterchainSecurityModule.
func (z *ZKExecutionISM) ModuleType() uint8 {
	return InterchainSecurityModuleTypeZKExecution
}

// Verify implements types.HyperlaneInterchainSecurityModule.
func (z *ZKExecutionISM) Verify(ctx context.Context, metadata []byte, message util.HyperlaneMessage) (bool, error) {
	return true, nil
}

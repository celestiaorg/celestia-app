package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// EventTypeSetFibreProviderInfo is the event type for setting fibre provider info
	EventTypeSetFibreProviderInfo = "set_fibre_provider_info"

	// AttributeKeyValidatorAddress is the attribute key for consensus address
	AttributeKeyValidatorAddress = "validator_consensus_address"
	// AttributeKeyHost is the attribute key for IP address
	AttributeKeyHost = "host"

	// MaxHostLen is the maximum length for the host field (IP address, DNS name, etc.)
	MaxHostLen = 100
)

var _ sdk.Msg = &MsgSetFibreProviderInfo{}

// ValidateBasic performs basic validation of the MsgSetFibreProviderInfo message
func (m *MsgSetFibreProviderInfo) ValidateBasic() error {
	// Validate validator address
	if m.Signer == "" {
		return errorsmod.Wrap(ErrInvalidValidator, "validator address cannot be empty")
	}
	_, err := sdk.ValAddressFromBech32(m.Signer)
	if err != nil {
		return errorsmod.Wrapf(ErrInvalidValidator, "invalid validator address: %v", err)
	}

	// Validate address length (supports IP addresses, DNS names, etc.)
	if len(m.Host) > MaxHostLen {
		return errorsmod.Wrapf(ErrInvalidHostAddress, "address must be less than 90 characters, got %d", len(m.Host))
	}
	if len(m.Host) == 0 {
		return errorsmod.Wrap(ErrInvalidHostAddress, "address cannot be empty")
	}

	return nil
}

package types

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// NewGenesisState creates a new GenesisState object
func NewGenesisState(providers []GenesisProvider) *GenesisState {
	return &GenesisState{
		Providers: providers,
	}
}

// DefaultGenesisState returns the default genesis state
func DefaultGenesisState() *GenesisState {
	return &GenesisState{
		Providers: []GenesisProvider{},
	}
}

// ValidateGenesis validates the fibre module genesis data
func ValidateGenesis(data GenesisState) error {
	seenValidators := make(map[string]bool)

	for _, provider := range data.Providers {
		if provider.ValidatorAddress == "" {
			return ErrInvalidValidatorAddress
		}

		// Validate validator address format
		_, err := sdk.ValAddressFromBech32(provider.ValidatorAddress)
		if err != nil {
			return ErrInvalidValidatorAddress
		}

		if seenValidators[provider.ValidatorAddress] {
			return ErrInvalidValidatorAddress
		}
		seenValidators[provider.ValidatorAddress] = true

		if len(provider.Info.IpAddress) == 0 {
			return ErrEmptyIPAddress
		}

		if len(provider.Info.IpAddress) > MaxIPAddressLength {
			return ErrIPAddressTooLong
		}
	}

	return nil
}
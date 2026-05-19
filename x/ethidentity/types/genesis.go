package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/ethereum/go-ethereum/common"
)

// GenesisState defines the ethidentity module genesis state.
type GenesisState struct {
	Mappings []Mapping `json:"mappings,omitempty"`
}

// Mapping is a derived same-key identity mapping from an Ethereum address to a
// canonical Celestia address.
type Mapping struct {
	EthereumAddress string `json:"ethereum_address"`
	CelestiaAddress string `json:"celestia_address"`
}

// DefaultGenesis returns an empty default genesis state.
func DefaultGenesis() GenesisState {
	return GenesisState{}
}

// ValidateGenesis validates ethidentity genesis state.
func ValidateGenesis(genesis GenesisState) error {
	seen := make(map[string]struct{}, len(genesis.Mappings))
	for _, mapping := range genesis.Mappings {
		if !common.IsHexAddress(mapping.EthereumAddress) {
			return fmt.Errorf("invalid Ethereum address %q", mapping.EthereumAddress)
		}
		if _, err := sdk.AccAddressFromBech32(mapping.CelestiaAddress); err != nil {
			return fmt.Errorf("invalid Celestia address for Ethereum address %s: %w", mapping.EthereumAddress, err)
		}

		ethAddr := common.HexToAddress(mapping.EthereumAddress).Hex()
		if _, ok := seen[ethAddr]; ok {
			return fmt.Errorf("duplicate Ethereum address %s", ethAddr)
		}
		seen[ethAddr] = struct{}{}
	}
	return nil
}

package valaddr

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v10/x/valaddr/keeper"
	"github.com/celestiaorg/celestia-app/v10/x/valaddr/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultGenesisState returns the default genesis state for the valaddr module
func DefaultGenesisState() *types.GenesisState {
	return &types.GenesisState{}
}

// ValidateGenesis validates the genesis state
func ValidateGenesis(data *types.GenesisState) error {
	if data == nil {
		return fmt.Errorf("genesis state cannot be nil")
	}

	seen := make(map[string]struct{}, len(data.FibreProviders))
	for _, provider := range data.FibreProviders {
		address, err := sdk.ConsAddressFromBech32(provider.ValidatorConsensusAddress)
		if err != nil {
			return fmt.Errorf("invalid validator consensus address %q: %w", provider.ValidatorConsensusAddress, err)
		}
		key := string(address)
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate validator consensus address %q", provider.ValidatorConsensusAddress)
		}
		seen[key] = struct{}{}
	}
	// Preserve historical hosts even if current transaction validation rejects them.
	return nil
}

// InitGenesis initializes the module's state from a genesis state
func InitGenesis(ctx sdk.Context, k keeper.Keeper, data *types.GenesisState) {
	if err := ValidateGenesis(data); err != nil {
		panic(err)
	}
	for _, provider := range data.FibreProviders {
		address, err := sdk.ConsAddressFromBech32(provider.ValidatorConsensusAddress)
		if err != nil {
			panic(err)
		}
		if err := k.SetFibreProviderInfo(ctx, address, provider.Info); err != nil {
			panic(err)
		}
	}
}

// ExportGenesis exports the module's state to a genesis state
func ExportGenesis(ctx sdk.Context, k keeper.Keeper) *types.GenesisState {
	genesis := DefaultGenesisState()
	err := k.IterateFibreProviderInfo(ctx, func(address sdk.ConsAddress, info types.FibreProviderInfo) bool {
		genesis.FibreProviders = append(genesis.FibreProviders, types.FibreProvider{
			ValidatorConsensusAddress: address.String(),
			Info:                      info,
		})
		return false
	})
	if err != nil {
		panic(err)
	}
	return genesis
}

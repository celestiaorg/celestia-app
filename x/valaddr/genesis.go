package valaddr

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v6/x/valaddr/keeper"
	"github.com/celestiaorg/celestia-app/v6/x/valaddr/types"
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

	return nil
}

// InitGenesis initializes the module's state from a genesis state
func InitGenesis(_ sdk.Context, _ keeper.Keeper, _ *types.GenesisState) {
}

// ExportGenesis exports the module's state to a genesis state
func ExportGenesis(_ sdk.Context, _ keeper.Keeper) *types.GenesisState {
	return &types.GenesisState{}
}

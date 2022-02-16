package keeper

import (
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// GetDelegateKeys iterates both the EthAddress and Orchestrator address indexes to produce
// a vector of MsgSetOrchestratorAddress entries containing all the delegate keys for state
// export / import.
func (k Keeper) GetDelegateKeys(ctx sdk.Context) []types.MsgSetOrchestratorAddress {
	// TODO
	return nil
}

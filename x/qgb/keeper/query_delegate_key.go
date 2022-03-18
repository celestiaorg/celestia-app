package keeper

import (
	"context"
	"github.com/celestiaorg/celestia-app/x/qgb/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

func (k Keeper) GetDelegateKeyByOrchestrator(
	c context.Context,
	req *types.QueryGetDelegateKeysByOrchestratorAddress) (*types.QueryGetDelegateKeyByOrchestratorResponse, error) {
	ctx := sdk.UnwrapSDKContext(c)
	keys := k.GetDelegateKeys(ctx)
	reqOrchestrator, err := sdk.AccAddressFromBech32(req.OrchestratorAddress)
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		keyOrchestrator, err := sdk.AccAddressFromBech32(key.Orchestrator)
		// this should be impossible due to the validate basic on the set orchestrator message
		if err != nil {
			panic("Invalid orchestrator addr in store!")
		}
		if reqOrchestrator.Equals(keyOrchestrator) {
			return &types.QueryGetDelegateKeyByOrchestratorResponse{ValidatorAddress: key.Validator, EthAddress: key.EthAddress}, nil
		}
	}
	return nil, sdkerrors.Wrap(types.ErrInvalid, "No validator")
}

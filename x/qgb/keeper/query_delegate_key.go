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
	reqValidator, err := sdk.ValAddressFromBech32(req.OrchestratorAddress)
	if err != nil {
		return nil, err
	}
	for _, key := range keys {
		keyValidator, err := sdk.ValAddressFromBech32(key.Validator)
		// this should be impossible due to the `validate basic` on the set orchestrator message
		if err != nil {
			panic("Invalid validator addr in store!")
		}
		if reqValidator.Equals(keyValidator) {
			return &types.QueryGetDelegateKeyByOrchestratorResponse{
				EthAddress:       key.EthAddress,
				ValidatorAddress: key.Orchestrator,
			}, nil
		}
	}

	return nil, sdkerrors.Wrap(types.ErrInvalid, "No validator")
}

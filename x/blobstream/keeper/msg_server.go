package keeper

import (
	"context"

	"cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v2/x/blobstream/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	staking "github.com/cosmos/cosmos-sdk/x/staking/types"
	gethcommon "github.com/ethereum/go-ethereum/common"
)

var _ types.MsgServer = msgServer{}

type msgServer struct {
	Keeper
}

// NewMsgServerImpl returns an implementation of the Blobstream MsgServer interface
// for the provided Keeper.
func NewMsgServerImpl(keeper Keeper) types.MsgServer {
	return &msgServer{Keeper: keeper}
}

// RegisterEVMAddress verifies that the validator exists on chain. It then stores the EVM address.
// If it already exists, it will simply overwrite the previous value
func (k Keeper) RegisterEVMAddress(goCtx context.Context, msg *types.MsgRegisterEVMAddress) (*types.MsgRegisterEVMAddressResponse, error) {
	ctx := sdk.UnwrapSDKContext(goCtx)

	valAddr, err := sdk.ValAddressFromBech32(msg.ValidatorAddress)
	if err != nil {
		return nil, err
	}

	evmAddr := gethcommon.HexToAddress(msg.EvmAddress)

	if _, exists := k.StakingKeeper.GetValidator(ctx, valAddr); !exists {
		return nil, staking.ErrNoValidatorFound
	}

	if !k.IsEVMAddressUnique(ctx, evmAddr) {
		return nil, errors.Wrapf(types.ErrEVMAddressAlreadyExists, "address %s", msg.EvmAddress)
	}

	k.SetEVMAddress(ctx, valAddr, evmAddr)

	return &types.MsgRegisterEVMAddressResponse{}, nil
}

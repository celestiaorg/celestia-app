package keeper

import (
	"context"

	"cosmossdk.io/collections"
	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warpkeeper "github.com/bcp-innovations/hyperlane-cosmos/x/warp/keeper"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/celestia-app/v8/x/forwarding/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Ensure WarpKeeperAdapter implements types.WarpKeeper
var _ types.WarpKeeper = (*WarpKeeperAdapter)(nil)

// WarpKeeperAdapter wraps the hyperlane-cosmos warp keeper to implement the
// WarpKeeper interface, providing method-based access to collections.Map fields.
type WarpKeeperAdapter struct {
	keeper *warpkeeper.Keeper
}

// NewWarpKeeperAdapter creates a new WarpKeeperAdapter wrapping the given keeper.
func NewWarpKeeperAdapter(k *warpkeeper.Keeper) *WarpKeeperAdapter {
	return &WarpKeeperAdapter{keeper: k}
}

func (a *WarpKeeperAdapter) RemoteTransferSynthetic(
	ctx sdk.Context,
	token warptypes.HypToken,
	cosmosSender string,
	destinationDomain uint32,
	recipient util.HexAddress,
	amount math.Int,
	customHookId *util.HexAddress,
	gasLimit math.Int,
	maxFee sdk.Coin,
	customHookMetadata []byte,
) (util.HexAddress, error) {
	return a.keeper.RemoteTransferSynthetic(ctx, token, cosmosSender, destinationDomain, recipient, amount, customHookId, gasLimit, maxFee, customHookMetadata)
}

func (a *WarpKeeperAdapter) RemoteTransferCollateral(
	ctx sdk.Context,
	token warptypes.HypToken,
	cosmosSender string,
	destinationDomain uint32,
	recipient util.HexAddress,
	amount math.Int,
	customHookId *util.HexAddress,
	gasLimit math.Int,
	maxFee sdk.Coin,
	customHookMetadata []byte,
) (util.HexAddress, error) {
	return a.keeper.RemoteTransferCollateral(ctx, token, cosmosSender, destinationDomain, recipient, amount, customHookId, gasLimit, maxFee, customHookMetadata)
}

func (a *WarpKeeperAdapter) GetHypToken(ctx context.Context, id uint64) (warptypes.HypToken, error) {
	return a.keeper.HypTokens.Get(ctx, id)
}

func (a *WarpKeeperAdapter) GetAllHypTokens(ctx context.Context) ([]warptypes.HypToken, error) {
	iter, err := a.keeper.HypTokens.Iterate(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer iter.Close()

	var tokens []warptypes.HypToken
	for ; iter.Valid(); iter.Next() {
		token, err := iter.Value()
		if err != nil {
			// Skip tokens that fail to decode, matching original behavior
			continue
		}
		tokens = append(tokens, token)
	}
	return tokens, nil
}

func (a *WarpKeeperAdapter) HasEnrolledRouter(ctx context.Context, tokenId uint64, domain uint32) (bool, error) {
	return a.keeper.EnrolledRouters.Has(ctx, collections.Join(tokenId, domain))
}

func (a *WarpKeeperAdapter) GetEnrolledRouter(ctx context.Context, tokenId uint64, domain uint32) (warptypes.RemoteRouter, error) {
	return a.keeper.EnrolledRouters.Get(ctx, collections.Join(tokenId, domain))
}

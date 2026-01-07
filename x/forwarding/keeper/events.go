package keeper

import (
	"github.com/celestiaorg/celestia-app/v6/x/forwarding/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// EmitUpdateISMEventEmitCreateInterchainAccountsRouterEvent ...
func EmitCreateInterchainAccountsRouterEvent(ctx sdk.Context, router types.InterchainAccountsRouter) error {
	return ctx.EventManager().EmitTypedEvent(&types.EventCreateInterchainAccountsRouter{
		Id:    router.Id,
		Owner: router.Owner,
	})
}

package types

import (
	"context"

	"cosmossdk.io/math"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
)

// AccountKeeper defines the expected account keeper interface
type AccountKeeper interface {
	GetModuleAddress(moduleName string) sdk.AccAddress
}

// BankKeeper defines the expected bank keeper interface
type BankKeeper interface {
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error
	SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
}

// WarpKeeper defines the expected warp keeper interface
// Note: For enrolled router access, use the concrete keeper's EnrolledRouters field directly
type WarpKeeper interface {
	// GetHypToken retrieves a HypToken by its ID
	GetHypToken(ctx context.Context, tokenId util.HexAddress) (warptypes.HypToken, error)

	// HasEnrolledRouter checks if a route exists for a token to a destination domain
	// Implementation note: The concrete keeper exposes EnrolledRouters as a public collections.Map field.
	// Access it directly via: keeper.EnrolledRouters.Has(ctx, collections.Join(tokenId.GetInternalId(), destDomain))
	// This method is provided by a wrapper for interface compatibility.

	// RemoteTransferSynthetic initiates a cross-chain transfer for synthetic tokens
	RemoteTransferSynthetic(
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
	) (util.HexAddress, error)

	// RemoteTransferCollateral initiates a cross-chain transfer for collateral tokens
	RemoteTransferCollateral(
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
	) (util.HexAddress, error)
}

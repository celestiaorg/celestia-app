package types

import (
	"context"

	"cosmossdk.io/math"
	"github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// HyperlaneKeeper defines the expected hyperlane core keeper interface for fee quoting.
type HyperlaneKeeper interface {
	// QuoteDispatch returns the required fee for dispatching a message
	QuoteDispatch(ctx context.Context, mailboxId util.HexAddress, overwriteHookId util.HexAddress, metadata util.StandardHookMetadata, message util.HyperlaneMessage) (sdk.Coins, error)
}

// BankKeeper defines the expected bank keeper interface
type BankKeeper interface {
	GetAllBalances(ctx context.Context, addr sdk.AccAddress) sdk.Coins
	GetBalance(ctx context.Context, addr sdk.AccAddress, denom string) sdk.Coin
	SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error
}

// WarpKeeper defines the expected warp keeper interface.
// This interface wraps the hyperlane-cosmos warp keeper, providing method-based
// access to state that the underlying keeper exposes via collections.Map fields.
type WarpKeeper interface {
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

	// GetHypToken retrieves a HypToken by its internal ID
	GetHypToken(ctx context.Context, id uint64) (warptypes.HypToken, error)

	// GetAllHypTokens returns all registered HypTokens
	GetAllHypTokens(ctx context.Context) ([]warptypes.HypToken, error)

	// HasEnrolledRouter checks if a token has an enrolled router for a destination domain
	HasEnrolledRouter(ctx context.Context, tokenId uint64, domain uint32) (bool, error)

	// GetEnrolledRouter retrieves the enrolled router for a token and destination domain
	GetEnrolledRouter(ctx context.Context, tokenId uint64, domain uint32) (warptypes.RemoteRouter, error)
}

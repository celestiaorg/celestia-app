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
	SendCoins(ctx context.Context, fromAddr, toAddr sdk.AccAddress, amt sdk.Coins) error
}

// WarpKeeper defines the expected warp keeper interface.
//
// NOTE: The forwarding keeper uses the concrete *warpkeeper.Keeper type because
// the hyperlane-cosmos warp keeper exposes state via public collections.Map fields
// (HypTokens, EnrolledRouters) which cannot be expressed in a Go interface.
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
}

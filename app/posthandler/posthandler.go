package posthandler

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
	authkeeper "github.com/cosmos/cosmos-sdk/x/auth/keeper"
	bankkeeper "github.com/cosmos/cosmos-sdk/x/bank/keeper"
	feegrantkeeper "github.com/cosmos/cosmos-sdk/x/feegrant/keeper"
)

// New returns a new posthandler chain.
func New(
	accountKeeper authkeeper.AccountKeeper,
	bankKeeper bankkeeper.Keeper,
	feegrantKeeper feegrantkeeper.Keeper,
) sdk.AnteHandler {
	postDecorators := []sdk.AnteDecorator{
		// The refund gas remaining decorator must be the last decorator in this
		// list.
		NewRefundGasRemainingDecorator(accountKeeper, bankKeeper, feegrantKeeper),
	}

	return sdk.ChainAnteDecorators(postDecorators...)
}

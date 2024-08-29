package posthandler

import (
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// New returns a new posthandler chain. Note: the Cosmos SDK does not export a
// type for PostHandler so the AnteHandler type is used.
func New() sdk.AnteHandler {
	postDecorators := []sdk.AnteDecorator{}
	return sdk.ChainAnteDecorators(postDecorators...)
}

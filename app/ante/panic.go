package ante

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

// HandlePanicDecorator that catches panics and wraps them in the transaction that caused the panic
type HandlePanicDecorator struct{}

func NewHandlePanicDecorator() HandlePanicDecorator {
	return HandlePanicDecorator{}
}

func (d HandlePanicDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (newCtx sdk.Context, err error) {
	defer func() {
		if r := recover(); r != nil {
			panic(fmt.Sprint(r, tx))
		}
	}()

	return next(newCtx, tx, simulate)
}

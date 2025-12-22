package ante

import (
	"fmt"
	"strings"

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
			panic(fmt.Sprint(r, FormatTx(tx)))
		}
	}()

	return next(ctx, tx, simulate)
}

func FormatTx(tx sdk.Tx) string {
	var output strings.Builder
	output.WriteString("\ncaused by transaction:\n")
	for _, msg := range tx.GetMsgs() {
		output.WriteString(fmt.Sprintf("%T{%s}\n", msg, msg))
	}
	return output.String()
}

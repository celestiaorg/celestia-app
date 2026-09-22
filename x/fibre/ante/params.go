package ante

import (
	errorsmod "cosmossdk.io/errors"
	"github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ParamsVersionDecorator keeps historical duration bounds in transaction admission.
type ParamsVersionDecorator struct{}

func (ParamsVersionDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	for _, msg := range tx.GetMsgs() {
		if update, ok := msg.(*types.MsgUpdateFibreParams); ok {
			if err := update.Params.ValidateForVersion(ctx.ConsensusParams().Version.GetApp()); err != nil {
				return ctx, errorsmod.Wrap(err, "invalid params")
			}
		}
	}
	return next(ctx, tx, simulate)
}

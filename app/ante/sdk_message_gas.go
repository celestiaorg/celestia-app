package ante

import (
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ sdk.AnteDecorator = SDKMessageGasDecorator{}

// SDKMessageCounter returns how many messages msgs execute, counting the
// messages that MsgExec and ICA packets dispatch. It must read no state: the
// gas it prices has to match between ProcessProposal and FinalizeBlock.
type SDKMessageCounter func(msgs []sdk.Msg) int

// SDKMessageGasDecorator charges GasPerAdditionalSDKMessage for every message a
// tx counts against MaxSDKMessages beyond the first. A wrapped or ICA message
// that fails at execution costs only its bytes, so without this charge one
// cheap tx could take the whole per-block budget and keep every other SDK tx
// out of the block. The first message is free, so single-message txs pay the
// same gas as before.
type SDKMessageGasDecorator struct {
	count SDKMessageCounter
}

func NewSDKMessageGasDecorator(count SDKMessageCounter) SDKMessageGasDecorator {
	return SDKMessageGasDecorator{count: count}
}

func (d SDKMessageGasDecorator) AnteHandle(ctx sdk.Context, tx sdk.Tx, simulate bool, next sdk.AnteHandler) (sdk.Context, error) {
	// The counter stops just past MaxSDKMessages and a single message weighs at
	// most what fits in a tx, so the product cannot overflow.
	if weight := d.count(tx.GetMsgs()); weight > 1 {
		ctx.GasMeter().ConsumeGas(uint64(weight-1)*appconsts.GasPerAdditionalSDKMessage, "ante: additional sdk messages")
	}
	return next(ctx, tx, simulate)
}

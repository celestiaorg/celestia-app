package types

import "cosmossdk.io/errors"

// ErrFeeForwardAmountNotFound is returned when the fee forward amount is not found in context.
// This indicates the MsgForwardFees was processed without the FeeForwardTerminatorDecorator running first.
var ErrFeeForwardAmountNotFound = errors.Register(ModuleName, 2, "fee forward amount not found in context")

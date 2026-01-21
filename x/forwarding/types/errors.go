package types

import (
	"cosmossdk.io/errors"
)

// Forwarding module sentinel errors
var (
	ErrAddressMismatch    = errors.Register(ModuleName, 2, "derived address does not match provided address")
	ErrNoBalance          = errors.Register(ModuleName, 3, "no balance at forwarding address")
	ErrUnsupportedToken   = errors.Register(ModuleName, 4, "unsupported token denom")
	ErrInvalidRecipient   = errors.Register(ModuleName, 5, "invalid recipient length")
	ErrNoWarpRoute        = errors.Register(ModuleName, 6, "no warp route to destination domain")
	ErrInsufficientIgpFee = errors.Register(ModuleName, 7, "IGP fee provided is less than required")
	ErrAllTokensFailed    = errors.Register(ModuleName, 8, "all tokens failed to forward")
)

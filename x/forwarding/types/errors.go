package types

import (
	"cosmossdk.io/errors"
)

// Forwarding module sentinel errors
var (
	ErrAddressMismatch    = errors.Register(ModuleName, 2, "derived address does not match provided address")
	ErrNoBalance          = errors.Register(ModuleName, 3, "no balance at forwarding address")
	ErrBelowMinimum       = errors.Register(ModuleName, 4, "balance below minimum threshold")
	ErrUnsupportedToken   = errors.Register(ModuleName, 5, "unsupported token denom")
	ErrTooManyTokens      = errors.Register(ModuleName, 6, "too many tokens at forwarding address")
	ErrInvalidRecipient   = errors.Register(ModuleName, 7, "invalid recipient length")
	ErrNoWarpRoute        = errors.Register(ModuleName, 8, "no warp route to destination domain")
	ErrInsufficientIgpFee = errors.Register(ModuleName, 9, "IGP fee provided is less than required")
)

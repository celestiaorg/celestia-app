package types

import (
	"cosmossdk.io/errors"
)

// Forwarding module sentinel errors
var (
	ErrAddressMismatch  = errors.Register(ModuleName, 1, "derived address does not match provided address")
	ErrNoBalance        = errors.Register(ModuleName, 2, "no balance at forwarding address")
	ErrBelowMinimum     = errors.Register(ModuleName, 3, "balance below minimum threshold")
	ErrUnsupportedToken = errors.Register(ModuleName, 4, "unsupported token denom")
	ErrTooManyTokens    = errors.Register(ModuleName, 5, "too many tokens at forwarding address")
)

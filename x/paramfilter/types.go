package paramfilter

import (
	sdkerrors "cosmossdk.io/errors"
)

const (
	// ModuleName is the name of the module
	ModuleName    = "paramfilter"
	baseErrorCode = 91710
)

var (
	// ErrForbiddenParameter is the error wrapped when a proposal to change a
	// forbidden parameter is submitted.
	ErrForbiddenParameter = sdkerrors.Register(ModuleName, baseErrorCode, "forbidden parameter change, hard fork required")
)

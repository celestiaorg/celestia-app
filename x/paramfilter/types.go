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
	// Error code for ErrForbiddenParameter
	ErrForbiddenParameter = sdkerrors.Register(ModuleName, baseErrorCode, "forbidden parameter change, hard fork required")
)

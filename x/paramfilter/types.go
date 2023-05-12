package paramfilter

import (
	sdkerrors "cosmossdk.io/errors"
)

const (
	// ModuleName is the name of the module
	ModuleName    = "paramfilter"
	baseErrorCode = 91710
)

// ErrBlockedParameter is the error wrapped when a proposal to change a
// blocked parameter is submitted.
var ErrBlockedParameter = sdkerrors.Register(ModuleName, baseErrorCode, "parameter can not be modified")

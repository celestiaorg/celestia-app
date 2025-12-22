package types

import (
	errorsmod "cosmossdk.io/errors"
)

// Module error codes scoped by ModuleName.
// NOTE: Error code 1 is reserved by cosmos-sdk as internal error / unknown failure

var (
	ErrInvalidRoute = errorsmod.Register(ModuleName, 2, "invalid route")
)

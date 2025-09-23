package types

import (
	errorsmod "cosmossdk.io/errors"
)

// Module error codes scoped by ModuleName.
// NOTE: Error code 1 is reserved by cosmos-sdk as internal error / unknown failure

var (
	ErrIsmNotFound         = errorsmod.Register(ModuleName, 2, "ism not found")
	ErrInvalidPublicValues = errorsmod.Register(ModuleName, 3, "invalid public values")
	ErrInvalidProof        = errorsmod.Register(ModuleName, 4, "invalid proof")
	ErrHeaderHashNotFound  = errorsmod.Register(ModuleName, 5, "header hash not found")
)

package types

// DONTCOVER

import (
	"cosmossdk.io/errors"
)

var (
	ErrInvalidValidatorAddress    = errors.Register(ModuleName, 12001, "invalid validator address")
	ErrValidatorNotActive         = errors.Register(ModuleName, 12002, "validator is not in active set")
	ErrInvalidIPAddress           = errors.Register(ModuleName, 12003, "invalid IP address")
	ErrIPAddressTooLong           = errors.Register(ModuleName, 12004, "IP address too long (max 45 characters)")
	ErrProviderInfoNotFound       = errors.Register(ModuleName, 12005, "fibre provider info not found")
	ErrUnauthorized               = errors.Register(ModuleName, 12006, "unauthorized: only validator can update their own info")
	ErrValidatorStillActive       = errors.Register(ModuleName, 12007, "cannot remove info for active validator")
	ErrEmptyIPAddress             = errors.Register(ModuleName, 12008, "IP address cannot be empty")
)
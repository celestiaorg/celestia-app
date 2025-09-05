package types

import "cosmossdk.io/errors"

// x/staking module sentinel errors
var (
	ErrHeaderHashNotFound = errors.Register(ModuleName, 2, "header hash not found")
)

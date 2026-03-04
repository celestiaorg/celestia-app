package types

import (
	"cosmossdk.io/errors"
)

var (
	ErrInvalidHostAddress = errors.Register(ModuleName, 1, "invalid host address")
	ErrInvalidValidator   = errors.Register(ModuleName, 2, "invalid validator")
)

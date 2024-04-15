package types

import (
	"cosmossdk.io/errors"
)

var ErrInvalidVersion = errors.Register(ModuleName, 1, "signalled version must be either the current version or one greater")

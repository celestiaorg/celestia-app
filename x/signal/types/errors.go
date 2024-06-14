package types

import (
	"cosmossdk.io/errors"
)

var ErrInvalidVersion = errors.Register(ModuleName, 1, "signalled version can not be less than the current version")

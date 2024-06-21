package types

import (
	"cosmossdk.io/errors"
)

var (
	ErrInvalidSignalVersion  = errors.Register(ModuleName, 1, "invalid signal version because signal version can not be less than the current version")
	ErrInvalidUpgradeVersion = errors.Register(ModuleName, 3, "invalid upgrade version")
	ErrUpgradePending        = errors.Register(ModuleName, 2, "upgrade is already pending")
)

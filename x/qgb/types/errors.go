package types

import sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

var (
	ErrInvalid           = sdkerrors.Register(ModuleName, 3, "invalid")
	ErrDuplicate         = sdkerrors.Register(ModuleName, 2, "duplicate")
	ErrUnknown           = sdkerrors.Register(ModuleName, 5, "unknown")
	ErrEmpty             = sdkerrors.Register(ModuleName, 6, "empty")
	ErrResetDelegateKeys = sdkerrors.Register(ModuleName, 10, "can not set orchestrator addresses more than once")
)

package types

import sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

var (
	ErrInvalid   = sdkerrors.Register(ModuleName, 3, "invalid")
	ErrDuplicate = sdkerrors.Register(ModuleName, 2, "duplicate")
	ErrResetDelegateKeys = sdkerrors.Register(ModuleName, 10, "can not set orchestrator addresses more than once")
)

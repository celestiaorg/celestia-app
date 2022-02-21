package types

import sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

var (
	ErrResetDelegateKeys = sdkerrors.Register(ModuleName, 10, "can not set orchestrator addresses more than once")
)

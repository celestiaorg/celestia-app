package types

import sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

var (
	ErrInvalid           = sdkerrors.Register(ModuleName, 3, "invalid")
	ErrDuplicate         = sdkerrors.Register(ModuleName, 2, "duplicate")
	ErrUnknown           = sdkerrors.Register(ModuleName, 5, "unknown")
	ErrEmpty             = sdkerrors.Register(ModuleName, 6, "empty")
	ErrResetDelegateKeys = sdkerrors.Register(ModuleName, 10, "can not set orchestrator addresses more than once")
	ErrNoValidators      = sdkerrors.Register(ModuleName, 12, "no bonded validators in active set")
	ErrInvalidValAddress = sdkerrors.Register(ModuleName, 13, "invalid validator address in current valset %v")
	ErrInvalidEthAddress = sdkerrors.Register(ModuleName, 14, "discovered invalid eth address stored for validator %v")
	ErrInvalidValset     = sdkerrors.Register(ModuleName, 15, "generated invalid valset")
)

// var (
// 	ErrInternal                = sdkerrors.Register(ModuleName, 1, "internal")
// 	ErrDuplicate               = sdkerrors.Register(ModuleName, 2, "duplicate")
// 	ErrInvalid                 = sdkerrors.Register(ModuleName, 3, "invalid")
// 	ErrTimeout                 = sdkerrors.Register(ModuleName, 4, "timeout")
// 	ErrUnknown                 = sdkerrors.Register(ModuleName, 5, "unknown")
// 	ErrEmpty                   = sdkerrors.Register(ModuleName, 6, "empty")
// 	ErrOutdated                = sdkerrors.Register(ModuleName, 7, "outdated")
// 	ErrUnsupported             = sdkerrors.Register(ModuleName, 8, "unsupported")
// 	ErrNonContiguousEventNonce = sdkerrors.Register(ModuleName, 9, "non contiguous event nonce")
// 	ErrResetDelegateKeys       = sdkerrors.Register(ModuleName, 10, "can not set orchestrator addresses more than once")
// 	ErrMismatched              = sdkerrors.Register(ModuleName, 11, "mismatched")

// 	ErrInvalidValAddress       = sdkerrors.Register(ModuleName, 13, "invalid validator address in current valset %v")

// 	ErrInvalidValset           = sdkerrors.Register(ModuleName, 15, "generated invalid valset")
// )

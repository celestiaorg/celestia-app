package types

import sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"

var (
	ErrInvalid                             = sdkerrors.Register(ModuleName, 3, "invalid")
	ErrDuplicate                           = sdkerrors.Register(ModuleName, 2, "duplicate")
	ErrUnknown                             = sdkerrors.Register(ModuleName, 5, "unknown")
	ErrEmpty                               = sdkerrors.Register(ModuleName, 6, "empty")
	ErrResetDelegateKeys                   = sdkerrors.Register(ModuleName, 10, "can not set orchestrator addresses more than once")
	ErrNoValidators                        = sdkerrors.Register(ModuleName, 12, "no bonded validators in active set")
	ErrInvalidValAddress                   = sdkerrors.Register(ModuleName, 13, "invalid validator address in current valset %v")
	ErrInvalidEthAddress                   = sdkerrors.Register(ModuleName, 14, "discovered invalid eth address stored for validator %v")
	ErrInvalidValset                       = sdkerrors.Register(ModuleName, 15, "generated invalid valset")
	ErrAttestationNotValsetRequest         = sdkerrors.Register(ModuleName, 16, "attestation is not a valset request")
	ErrAttestationNotDataCommitmentRequest = sdkerrors.Register(ModuleName, 17, "attestation is not a data commitment request")
	ErrAttestationNotFound                 = sdkerrors.Register(ModuleName, 18, "attestation not found")
	ErrNilDataCommitmentRequest            = sdkerrors.Register(ModuleName, 19, "data commitment cannot be nil when setting attestation") //nolint:lll
	ErrNilValsetRequest                    = sdkerrors.Register(ModuleName, 20, "valset cannot be nil when setting attestation")
)

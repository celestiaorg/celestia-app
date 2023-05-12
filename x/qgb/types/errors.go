package types

import (
	"cosmossdk.io/errors"
)

var (
	ErrDuplicate                                 = errors.Register(ModuleName, 1002, "duplicate")
	ErrEmpty                                     = errors.Register(ModuleName, 1006, "empty")
	ErrNoValidators                              = errors.Register(ModuleName, 1012, "no bonded validators in active set")
	ErrInvalidValAddress                         = errors.Register(ModuleName, 1013, "invalid validator address in current valset %v")
	ErrInvalidEVMAddress                         = errors.Register(ModuleName, 1014, "discovered invalid EVM address stored for validator %v")
	ErrInvalidValset                             = errors.Register(ModuleName, 1015, "generated invalid valset")
	ErrAttestationNotValsetRequest               = errors.Register(ModuleName, 1016, "attestation is not a valset request")
	ErrAttestationNotFound                       = errors.Register(ModuleName, 1018, "attestation not found")
	ErrNilAttestation                            = errors.Register(ModuleName, 1022, "nil attestation")
	ErrUnmarshalllAttestation                    = errors.Register(ModuleName, 1026, "couldn't unmarshall attestation from store")
	ErrNonceHigherThanLatestAttestationNonce     = errors.Register(ModuleName, 1027, "the provided nonce is higher than the latest attestation nonce")
	ErrNoValsetBeforeNonceOne                    = errors.Register(ModuleName, 1028, "there is no valset before attestation nonce 1")
	ErrDataCommitmentNotGenerated                = errors.Register(ModuleName, 1029, "no data commitment has been generated for the provided height")
	ErrDataCommitmentNotFound                    = errors.Register(ModuleName, 1030, "data commitment not found")
	ErrLatestAttestationNonceStillNotInitialized = errors.Register(ModuleName, 1031, "the latest attestation nonce has still not been defined in store")
	ErrInvalidDataCommitmentWindow               = errors.Register(ModuleName, 1032, "invalid data commitment window")
)

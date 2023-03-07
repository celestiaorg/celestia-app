package types

import (
	"cosmossdk.io/errors"
)

var (
	ErrDuplicate                             = errors.Register(ModuleName, 2, "duplicate")
	ErrEmpty                                 = errors.Register(ModuleName, 6, "empty")
	ErrNoValidators                          = errors.Register(ModuleName, 12, "no bonded validators in active set")
	ErrInvalidValAddress                     = errors.Register(ModuleName, 13, "invalid validator address in current valset %v")
	ErrInvalidEVMAddress                     = errors.Register(ModuleName, 14, "discovered invalid EVM address stored for validator %v")
	ErrInvalidValset                         = errors.Register(ModuleName, 15, "generated invalid valset")
	ErrAttestationNotValsetRequest           = errors.Register(ModuleName, 16, "attestation is not a valset request")
	ErrAttestationNotFound                   = errors.Register(ModuleName, 18, "attestation not found")
	ErrNilAttestation                        = errors.Register(ModuleName, 22, "nil attestation")
	ErrUnmarshalllAttestation                = errors.Register(ModuleName, 26, "couldn't unmarshall attestation from store")
	ErrNonceHigherThanLatestAttestationNonce = errors.Register(ModuleName, 27, "the provided nonce is higher than the latest attestation nonce")
	ErrNoValsetBeforeNonceOne                = errors.Register(ModuleName, 28, "there is no valset before attestation nonce 1")
	ErrDataCommitmentNotGenerated            = errors.Register(ModuleName, 29, "no data commitment has been generated for the provided height")
	ErrDataCommitmentNotFound                = errors.Register(ModuleName, 30, "data commitment not found")
)

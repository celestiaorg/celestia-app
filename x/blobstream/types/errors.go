package types

import (
	"cosmossdk.io/errors"
)

var (
	ErrDuplicate                                 = errors.Register(ModuleName, 2, "duplicate")
	ErrEmpty                                     = errors.Register(ModuleName, 6, "empty")
	ErrNoValidators                              = errors.Register(ModuleName, 12, "no bonded validators in active set")
	ErrInvalidValAddress                         = errors.Register(ModuleName, 13, "invalid validator address in current valset %v")
	ErrInvalidEVMAddress                         = errors.Register(ModuleName, 14, "discovered invalid EVM address stored for validator %v")
	ErrInvalidValset                             = errors.Register(ModuleName, 15, "generated invalid valset")
	ErrAttestationNotValsetRequest               = errors.Register(ModuleName, 16, "attestation is not a valset request")
	ErrAttestationNotFound                       = errors.Register(ModuleName, 18, "attestation not found")
	ErrNilAttestation                            = errors.Register(ModuleName, 22, "nil attestation")
	ErrUnmarshalllAttestation                    = errors.Register(ModuleName, 26, "couldn't unmarshall attestation from store")
	ErrNonceHigherThanLatestAttestationNonce     = errors.Register(ModuleName, 27, "the provided nonce is higher than the latest attestation nonce")
	ErrNoValsetBeforeNonceOne                    = errors.Register(ModuleName, 28, "there is no valset before attestation nonce 1")
	ErrDataCommitmentNotGenerated                = errors.Register(ModuleName, 29, "no data commitment has been generated for the provided height")
	ErrDataCommitmentNotFound                    = errors.Register(ModuleName, 30, "data commitment not found")
	ErrLatestAttestationNonceStillNotInitialized = errors.Register(ModuleName, 31, "the latest attestation nonce has still not been defined in store")
	ErrInvalidDataCommitmentWindow               = errors.Register(ModuleName, 32, "invalid data commitment window")
	ErrEarliestAvailableNonceStillNotInitialized = errors.Register(ModuleName, 33, "the earliest available nonce after pruning has still not been defined in store")
	ErrRequestedNonceWasPruned                   = errors.Register(ModuleName, 34, "the requested nonce has been pruned")
	ErrUnknownAttestationType                    = errors.Register(ModuleName, 35, "unknown attestation type")
	ErrEVMAddressNotHex                          = errors.Register(ModuleName, 36, "the provided evm address is not a valid hex address")
	ErrEVMAddressAlreadyExists                   = errors.Register(ModuleName, 37, "the provided evm address already exists")
	ErrEVMAddressNotFound                        = errors.Register(ModuleName, 38, "EVM address not found")
)

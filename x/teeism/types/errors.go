package types

import (
	errorsmod "cosmossdk.io/errors"
)

// Module error codes scoped by ModuleName.
// NOTE: Error code 1 is reserved by cosmos-sdk as internal error / unknown failure.
var (
	ErrIsmNotFound              = errorsmod.Register(ModuleName, 2, "ism not found")
	ErrInvalidQuote             = errorsmod.Register(ModuleName, 3, "invalid tdx quote")
	ErrInvalidCollateral        = errorsmod.Register(ModuleName, 4, "invalid attestation collateral")
	ErrMalformedEventLog        = errorsmod.Register(ModuleName, 5, "malformed dstack event log")
	ErrIdentityMismatch         = errorsmod.Register(ModuleName, 6, "quote does not come from the pinned enclave")
	ErrInvalidEnclaveIdentity   = errorsmod.Register(ModuleName, 7, "invalid enclave identity")
	ErrMalformedPayload         = errorsmod.Register(ModuleName, 8, "malformed attested payload")
	ErrPayloadNotAttested       = errorsmod.Register(ModuleName, 9, "report data does not commit to the payload")
	ErrInvalidTransition        = errorsmod.Register(ModuleName, 10, "invalid state transition")
	ErrStaleAttestation         = errorsmod.Register(ModuleName, 11, "attestation is stale or postdated")
	ErrInvalidTrustedState      = errorsmod.Register(ModuleName, 12, "invalid trusted state")
	ErrInvalidMerkleTreeAddress = errorsmod.Register(ModuleName, 13, "invalid merkle tree address")
	ErrMessageTooLarge          = errorsmod.Register(ModuleName, 14, "submitted field exceeds its maximum size")
)

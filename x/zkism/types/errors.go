package types

import (
	errorsmod "cosmossdk.io/errors"
)

// Module error codes scoped by ModuleName.
// NOTE: Error code 1 is reserved by cosmos-sdk as internal error / unknown failure

var (
	ErrIsmNotFound              = errorsmod.Register(ModuleName, 2, "ism not found")
	ErrInvalidVerifyingKey      = errorsmod.Register(ModuleName, 3, "invalid verifying key")
	ErrInvalidProof             = errorsmod.Register(ModuleName, 4, "invalid proof")
	ErrInvalidProofLength       = errorsmod.Register(ModuleName, 5, "invalid proof length")
	ErrInvalidProofPrefix       = errorsmod.Register(ModuleName, 6, "invalid proof prefix")
	ErrInvalidTrustedState      = errorsmod.Register(ModuleName, 7, "invalid trusted state")
	ErrInvalidStateRoot         = errorsmod.Register(ModuleName, 8, "invalid state root")
	ErrInvalidMerkleTreeAddress = errorsmod.Register(ModuleName, 9, "invalid merkle tree address")
	ErrMessageProofAlreadySubmitted = errorsmod.Register(ModuleName, 10, "message proof already submitted for current state root")
)

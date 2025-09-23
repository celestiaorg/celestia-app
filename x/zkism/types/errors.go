package types

import (
	errorsmod "cosmossdk.io/errors"
)

// Module error codes scoped by ModuleName.
// NOTE: Error code 1 is reserved by cosmos-sdk as internal error / unknown failure

var (
	ErrIsmNotFound         = errorsmod.Register(ModuleName, 2, "ism not found")
	ErrHeaderHashNotFound  = errorsmod.Register(ModuleName, 3, "header hash not found")
	ErrInvalidVerifyingKey = errorsmod.Register(ModuleName, 4, "invalid verifying key")
	ErrInvalidProof        = errorsmod.Register(ModuleName, 5, "invalid proof")
	ErrInvalidProofLength  = errorsmod.Register(ModuleName, 6, "invalid proof length")
	ErrInvalidProofPrefix  = errorsmod.Register(ModuleName, 7, "invalid proof prefix")
	ErrInvalidHeaderHash   = errorsmod.Register(ModuleName, 8, "invalid header hash")
	ErrInvalidStateRoot    = errorsmod.Register(ModuleName, 9, "invalid state root")
	ErrInvalidHeight       = errorsmod.Register(ModuleName, 10, "invalid trusted height")
	ErrInvalidNamespace    = errorsmod.Register(ModuleName, 11, "invalid namespace")
	ErrInvalidSequencerKey = errorsmod.Register(ModuleName, 12, "invalid sequencer public key")
)

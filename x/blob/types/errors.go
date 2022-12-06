package types

// DONTCOVER

import (
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	ErrReservedNamespace              = sdkerrors.Register(ModuleName, 11110, "cannot use reserved namespace IDs")
	ErrInvalidNamespaceLen            = sdkerrors.Register(ModuleName, 11111, "invalid namespace length")
	ErrInvalidDataSize                = sdkerrors.Register(ModuleName, 11112, "data must be multiple of shareSize")
	ErrDeclaredActualDataSizeMismatch = sdkerrors.Register(ModuleName, 11113, "declared data size does not match actual size")
	ErrCommittedSquareSizeNotPowOf2   = sdkerrors.Register(ModuleName, 11114, "committed to invalid square size: must be power of two")
	ErrCalculateCommit                = sdkerrors.Register(ModuleName, 11115, "unexpected error calculating commit for share")
	ErrInvalidShareCommit             = sdkerrors.Register(ModuleName, 11116, "invalid commit for share")
	ErrParitySharesNamespace          = sdkerrors.Register(ModuleName, 11117, "cannot use parity shares namespace ID")
	ErrTailPaddingNamespace           = sdkerrors.Register(ModuleName, 11118, "cannot use tail padding namespace ID")
	ErrTxNamespace                    = sdkerrors.Register(ModuleName, 11119, "cannot use transaction namespace ID")
	ErrEvidenceNamespace              = sdkerrors.Register(ModuleName, 11120, "cannot use evidence namespace ID")
	ErrEmptyShareCommitment           = sdkerrors.Register(ModuleName, 11121, "empty share commitment")
	ErrInvalidShareCommitments        = sdkerrors.Register(ModuleName, 11122, "invalid share commitments: all relevant square sizes must be committed to")
	ErrUnsupportedShareVersion        = sdkerrors.Register(ModuleName, 11123, "unsupported share version")
	ErrZeroBlobSize                   = sdkerrors.Register(ModuleName, 11124, "cannot use zero blob size")
)

package types

// DONTCOVER

import (
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
)

var (
	ErrReservedNamespace              = sdkerrors.Register(ModuleName, 11110, "cannot use reserved namespace IDs")
	ErrInvalidNamespaceLen            = sdkerrors.Register(ModuleName, 11111, "invalid namespace length")
	ErrInvalidDataSize                = sdkerrors.Register(ModuleName, 11112, "data must be multiple of shareSize")
	ErrBlobSizeMismatch               = sdkerrors.Register(ModuleName, 11113, "actual blob size differs from that specified in the MsgPayForBlob")
	ErrCommittedSquareSizeNotPowOf2   = sdkerrors.Register(ModuleName, 11114, "committed to invalid square size: must be power of two")
	ErrCalculateCommitment            = sdkerrors.Register(ModuleName, 11115, "unexpected error calculating commitment for share")
	ErrInvalidShareCommitment         = sdkerrors.Register(ModuleName, 11116, "invalid commitment for share")
	ErrParitySharesNamespace          = sdkerrors.Register(ModuleName, 11117, "cannot use parity shares namespace ID")
	ErrTailPaddingNamespace           = sdkerrors.Register(ModuleName, 11118, "cannot use tail padding namespace ID")
	ErrTxNamespace                    = sdkerrors.Register(ModuleName, 11119, "cannot use transaction namespace ID")
	ErrEvidenceNamespace              = sdkerrors.Register(ModuleName, 11120, "cannot use evidence namespace ID")
	ErrEmptyShareCommitment           = sdkerrors.Register(ModuleName, 11121, "empty share commitment")
	ErrInvalidShareCommitments        = sdkerrors.Register(ModuleName, 11122, "invalid share commitments: all relevant square sizes must be committed to")
	ErrUnsupportedShareVersion        = sdkerrors.Register(ModuleName, 11123, "unsupported share version")
	ErrZeroBlobSize                   = sdkerrors.Register(ModuleName, 11124, "cannot use zero blob size")
	ErrMismatchedNumberOfPFBorBlob    = sdkerrors.Register(ModuleName, 11125, "mismatched number of blobs per MsgPayForBlob")
	ErrNoPFB                          = sdkerrors.Register(ModuleName, 11126, "no MsgPayForBlobs found in blob transaction")
	ErrNamespaceMismatch              = sdkerrors.Register(ModuleName, 11127, "namespace of blob and its respective MsgPayForBlobs differ")
	ErrProtoParsing                   = sdkerrors.Register(ModuleName, 11128, "failure to parse a transaction from its protobuf representation")
	ErrMultipleMsgsInBlobTx           = sdkerrors.Register(ModuleName, 11129, "not yet supported: multiple sdk.Msgs found in BlobTx")
	ErrMismatchedNumberOfPFBComponent = sdkerrors.Register(ModuleName, 11130, "number of each component in a MsgPayForBlobs must be identical")
	ErrNoBlobs                        = sdkerrors.Register(ModuleName, 11131, "no blobs provided")
	ErrNoNamespaceIds                 = sdkerrors.Register(ModuleName, 11132, "no namespace IDs provided")
	ErrNoShareVersions                = sdkerrors.Register(ModuleName, 11133, "no share versions provided")
	ErrNoBlobSizes                    = sdkerrors.Register(ModuleName, 11134, "no blob sizes provided")
	ErrNoShareCommitments             = sdkerrors.Register(ModuleName, 11135, "no share commitments provided")
)

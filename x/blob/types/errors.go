package types

// DONTCOVER

import (
	"cosmossdk.io/errors"
)

var (
	ErrReservedNamespace              = errors.Register(ModuleName, 11110, "cannot use reserved namespace IDs")
	ErrInvalidNamespaceLen            = errors.Register(ModuleName, 11111, "invalid namespace length")
	ErrInvalidDataSize                = errors.Register(ModuleName, 11112, "data must be multiple of shareSize")
	ErrBlobSizeMismatch               = errors.Register(ModuleName, 11113, "actual blob size differs from that specified in the MsgPayForBlob")
	ErrCommittedSquareSizeNotPowOf2   = errors.Register(ModuleName, 11114, "committed to invalid square size: must be power of two")
	ErrCalculateCommitment            = errors.Register(ModuleName, 11115, "unexpected error calculating commitment for share")
	ErrInvalidShareCommitment         = errors.Register(ModuleName, 11116, "invalid commitment for share")
	ErrParitySharesNamespace          = errors.Register(ModuleName, 11117, "cannot use parity shares namespace ID")
	ErrTailPaddingNamespace           = errors.Register(ModuleName, 11118, "cannot use tail padding namespace ID")
	ErrTxNamespace                    = errors.Register(ModuleName, 11119, "cannot use transaction namespace ID")
	ErrInvalidShareCommitments        = errors.Register(ModuleName, 11122, "invalid share commitments: all relevant square sizes must be committed to")
	ErrUnsupportedShareVersion        = errors.Register(ModuleName, 11123, "unsupported share version")
	ErrZeroBlobSize                   = errors.Register(ModuleName, 11124, "cannot use zero blob size")
	ErrMismatchedNumberOfPFBorBlob    = errors.Register(ModuleName, 11125, "mismatched number of blobs per MsgPayForBlob")
	ErrNoPFB                          = errors.Register(ModuleName, 11126, "no MsgPayForBlobs found in blob transaction")
	ErrNamespaceMismatch              = errors.Register(ModuleName, 11127, "namespace of blob and its respective MsgPayForBlobs differ")
	ErrProtoParsing                   = errors.Register(ModuleName, 11128, "failure to parse a transaction from its protobuf representation")
	ErrMultipleMsgsInBlobTx           = errors.Register(ModuleName, 11129, "not yet supported: multiple sdk.Msgs found in BlobTx")
	ErrMismatchedNumberOfPFBComponent = errors.Register(ModuleName, 11130, "number of each component in a MsgPayForBlobs must be identical")
	ErrNoBlobs                        = errors.Register(ModuleName, 11131, "no blobs provided")
	ErrNoNamespaces                   = errors.Register(ModuleName, 11132, "no namespaces provided")
	ErrNoShareVersions                = errors.Register(ModuleName, 11133, "no share versions provided")
	ErrNoBlobSizes                    = errors.Register(ModuleName, 11134, "no blob sizes provided")
	ErrNoShareCommitments             = errors.Register(ModuleName, 11135, "no share commitments provided")
	ErrInvalidNamespace               = errors.Register(ModuleName, 11136, "invalid namespace")
	ErrInvalidNamespaceVersion        = errors.Register(ModuleName, 11137, "invalid namespace version")
	// ErrTotalBlobSize is deprecated, use ErrBlobsTooLarge instead.
	ErrTotalBlobSizeTooLarge = errors.Register(ModuleName, 11138, "total blob size too large")
	ErrBlobsTooLarge         = errors.Register(ModuleName, 11139, "blob(s) too large")
)

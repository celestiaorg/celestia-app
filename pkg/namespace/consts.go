package namespace

import (
	"bytes"
	"math"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
)

const (
	// NamespaveVersionSize is the size of a namespace version in bytes.
	NamespaceVersionSize = appconsts.NamespaceVersionSize

	// NamespaceIDSize is the size of a namespace ID in bytes.
	NamespaceIDSize = appconsts.NamespaceIDSize

	// NamespaceSize is the size of a namespace (version + ID) in bytes.
	NamespaceSize = appconsts.NamespaceSize

	// NamespaceVersionZero is the first namespace version.
	NamespaceVersionZero = uint8(0)

	// NamespaceVersionMax is the max namespace version.
	NamespaceVersionMax = math.MaxUint8

	// NamespaceZeroPrefixSize is the number of `0` bytes that are prefixed to
	// namespace IDs for version 0.
	NamespaceVersionZeroPrefixSize = 18

	// NamespaceVersionZeroIDSize is the number of bytes available for
	// user-specified namespace ID in a namespace ID for version 0.
	NamespaceVersionZeroIDSize = NamespaceIDSize - NamespaceVersionZeroPrefixSize
)

var (
	// NamespaceVersionZeroPrefix is the prefix of a namespace ID for version 0.
	NamespaceVersionZeroPrefix = bytes.Repeat([]byte{0}, NamespaceVersionZeroPrefixSize)

	// TxNamespace is the namespace reserved for transaction data.
	TxNamespace = MustNewV0([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 1})

	// IntermediateStateRootsNamespace is the namespace reserved for
	// intermediate state root data.
	IntermediateStateRootsNamespace = MustNewV0([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 2})

	// PayForBlobNamespace is the namespace reserved for PayForBlobs transactions.
	PayForBlobNamespace = MustNewV0([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 4})

	// ReservedPaddingNamespace is the namespace used for padding after all
	// reserved namespaces. In practice this padding is after transactions
	// (ordinary and PFBs) but before blobs.
	ReservedPaddingNamespace = MustNewV0([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 255})

	// MaxPrimaryReservedNamespace represents the largest primary reserved
	// namespace reserved for protocol use. Note that there may be other
	// non-primary reserved namespaces beyond this upper limit.
	MaxPrimaryReservedNamespace = MustNewV0([]byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 255})

	// TailPaddingNamespace is the namespace reserved for tail padding. All data
	// with this namespace will be ignored.
	TailPaddingNamespace = Namespace{
		Version: math.MaxUint8,
		ID:      append(bytes.Repeat([]byte{0xFF}, NamespaceIDSize-1), 0xFE),
	}

	// ParitySharesNamespace is the namespace reserved for erasure coded data.
	ParitySharesNamespace = Namespace{
		Version: math.MaxUint8,
		ID:      bytes.Repeat([]byte{0xFF}, NamespaceIDSize),
	}

	// SupportedBlobNamespaceVersions is a list of namespace versions that can be specified by a user for blobs.
	SupportedBlobNamespaceVersions = []uint8{NamespaceVersionZero}
)

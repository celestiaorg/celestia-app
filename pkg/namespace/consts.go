package namespace

import (
	"bytes"
	"math"
)

const (
	// NamespaveVersionSize is the size of a namespace version in bytes.
	NamespaceVersionSize = 1

	// NamespaceIDSize is the size of a namespace ID in bytes.
	NamespaceIDSize = 28

	// NamespaceSize is the size of a namespace (version + ID) in bytes.
	NamespaceSize = NamespaceVersionSize + NamespaceIDSize

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

	// TxNamespace is the namespace reserved for ordinary Cosmos SDK transactions.
	TxNamespace = primaryReservedNamespace(0x01)

	// IntermediateStateRootsNamespace is the namespace reserved for
	// intermediate state root data.
	IntermediateStateRootsNamespace = primaryReservedNamespace(0x02)

	// PayForBlobNamespace is the namespace reserved for PayForBlobs transactions.
	PayForBlobNamespace = primaryReservedNamespace(0x04)

	// PrimaryReservedPaddingNamespace is the namespace used for padding after all
	// primary reserved namespaces.
	PrimaryReservedPaddingNamespace = primaryReservedNamespace(0xFF)

	// MaxPrimaryReservedNamespace is the highest primary reserved namespace.
	// Namespaces lower than this are reserved for protocol use.
	MaxPrimaryReservedNamespace = primaryReservedNamespace(0xFF)

	// MinSecondaryReservedNamespace is the lowest secondary reserved namespace
	// reserved for protocol use. Namespaces higher than this are reserved for
	// protocol use.
	MinSecondaryReservedNamespace = secondaryReservedNamespace(0x00)

	// TailPaddingNamespace is the namespace reserved for tail padding. All data
	// with this namespace will be ignored.
	TailPaddingNamespace = secondaryReservedNamespace(0xFE)

	// ParitySharesNamespace is the namespace reserved for erasure coded data.
	ParitySharesNamespace = secondaryReservedNamespace(0xFF)

	// SupportedBlobNamespaceVersions is a list of namespace versions that can be specified by a user for blobs.
	SupportedBlobNamespaceVersions = []uint8{NamespaceVersionZero}
)

func primaryReservedNamespace(lastByte byte) Namespace {
	return Namespace{
		Version: NamespaceVersionZero,
		ID:      append(bytes.Repeat([]byte{0x00}, NamespaceIDSize-1), lastByte),
	}
}

func secondaryReservedNamespace(lastByte byte) Namespace {
	return Namespace{
		Version: NamespaceVersionMax,
		ID:      append(bytes.Repeat([]byte{0xFF}, NamespaceIDSize-1), lastByte),
	}
}

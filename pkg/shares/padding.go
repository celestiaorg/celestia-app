package shares

import (
	"bytes"
	"encoding/binary"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
)

// NamespacePaddingShare returns a share that acts as padding. Namespace padding
// shares follow a blob so that the next blob may start at an index that
// conforms to non-interactive default rules. The ns parameter provided should
// be the namespace of the blob that precedes this padding in the data square.
func NamespacePaddingShare(ns namespace.ID) Share {
	infoByte, err := NewInfoByte(appconsts.ShareVersionZero, true)
	if err != nil {
		panic(err)
	}

	sequenceLen := make([]byte, appconsts.SequenceLenBytes)
	binary.BigEndian.PutUint32(sequenceLen, uint32(0))

	padding := bytes.Repeat([]byte{0}, appconsts.FirstSparseShareContentSize)

	share := make([]byte, 0, appconsts.ShareSize)
	share = append(share, ns...)
	share = append(share, byte(infoByte))
	share = append(share, sequenceLen...)
	share = append(share, padding...)
	return share
}

// NamespacePaddingShares returns n namespace padding shares.
func NamespacePaddingShares(ns namespace.ID, n int) []Share {
	shares := make([]Share, n)
	for i := 0; i < n; i++ {
		shares[i] = NamespacePaddingShare(ns)
	}
	return shares
}

// ReservedPaddingShare returns a share that acts as padding. Reserved padding
// shares follow all significant shares in the reserved namespace so that the
// first blob can start at an index that conforms to non-interactive default
// rules.
func ReservedPaddingShare() Share {
	return NamespacePaddingShare(appconsts.ReservedPaddingNamespaceID)
}

// ReservedPaddingShare returns n reserved padding shares.
func ReservedPaddingShares(n int) []Share {
	return NamespacePaddingShares(appconsts.ReservedPaddingNamespaceID, n)
}

// TailPaddingShare is a share that is used to pad a data square to the desired
// square size. Tail padding shares follow the last blob share in the data
// square.
func TailPaddingShare() Share {
	return NamespacePaddingShare(appconsts.TailPaddingNamespaceID)
}

// TailPaddingShares returns n tail padding shares.
func TailPaddingShares(n int) []Share {
	return NamespacePaddingShares(appconsts.TailPaddingNamespaceID, n)
}

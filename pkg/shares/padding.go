package shares

import (
	"bytes"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
)

// NamespacePaddingShare returns a share that acts as padding. Namespace padding
// shares follow a blob so that the next blob may start at an index that
// conforms to non-interactive default rules. The ns parameter provided should
// be the namespace of the blob that precedes this padding in the data square.
func NamespacePaddingShare(ns namespace.ID) Share {
	b := NewBuilder(ns, appconsts.ShareVersionZero, true)
	if err := b.WriteSequenceLen(0); err != nil {
		panic(err)
	}
	padding := bytes.Repeat([]byte{0}, appconsts.FirstSparseShareContentSize)
	b.AddData(padding)

	share, err := b.Build()
	if err != nil {
		panic(err)
	}

	return *share
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

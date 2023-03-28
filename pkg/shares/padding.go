package shares

import (
	"bytes"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
)

// NamespacePaddingShare returns a share that acts as padding. Namespace padding
// shares follow a blob so that the next blob may start at an index that
// conforms to non-interactive default rules. The ns parameter provided should
// be the namespace of the blob that precedes this padding in the data square.
func NamespacePaddingShare(ns appns.Namespace) (Share, error) {
	b, err := NewBuilder(ns, appconsts.ShareVersionZero, true).Init()
	if err != nil {
		return Share{}, err
	}
	if err := b.WriteSequenceLen(0); err != nil {
		return Share{}, err
	}
	padding := bytes.Repeat([]byte{0}, appconsts.FirstSparseShareContentSize)
	b.AddData(padding)

	share, err := b.Build()
	if err != nil {
		return Share{}, err
	}

	return *share, nil
}

// NamespacePaddingShares returns n namespace padding shares.
func NamespacePaddingShares(ns appns.Namespace, n int) ([]Share, error) {
	var err error
	shares := make([]Share, n)
	for i := 0; i < n; i++ {
		shares[i], err = NamespacePaddingShare(ns)
		if err != nil {
			return shares, err
		}
	}
	return shares, nil
}

// ReservedPaddingShare returns a share that acts as padding. Reserved padding
// shares follow all significant shares in the reserved namespace so that the
// first blob can start at an index that conforms to non-interactive default
// rules.
func ReservedPaddingShare() (Share, error) {
	return NamespacePaddingShare(appns.ReservedPaddingNamespace)
}

// ReservedPaddingShare returns n reserved padding shares.
func ReservedPaddingShares(n int) ([]Share, error) {
	return NamespacePaddingShares(appns.ReservedPaddingNamespace, n)
}

// TailPaddingShare is a share that is used to pad a data square to the desired
// square size. Tail padding shares follow the last blob share in the data
// square.
func TailPaddingShare() (Share, error) {
	return NamespacePaddingShare(appns.TailPaddingNamespace)
}

// TailPaddingShares returns n tail padding shares.
func TailPaddingShares(n int) ([]Share, error) {
	return NamespacePaddingShares(appns.TailPaddingNamespace, n)
}

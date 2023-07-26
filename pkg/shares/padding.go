package shares

import (
	"bytes"
	"errors"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
)

// NamespacePaddingShare returns a share that acts as padding. Namespace padding
// shares follow a blob so that the next blob may start at an index that
// conforms to blob share commitment rules. The ns and shareVersion parameters
// provided should be the namespace and shareVersion of the blob that precedes
// this padding in the data square.
func NamespacePaddingShare(ns appns.Namespace, shareVersion uint8) (Share, error) {
	b, err := NewBuilder(ns, shareVersion, true).Init()
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
func NamespacePaddingShares(ns appns.Namespace, shareVersion uint8, n int) ([]Share, error) {
	var err error
	if n < 0 {
		return nil, errors.New("n must be positive")
	}
	shares := make([]Share, n)
	for i := 0; i < n; i++ {
		shares[i], err = NamespacePaddingShare(ns, shareVersion)
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
func ReservedPaddingShare() Share {
	share, err := NamespacePaddingShare(appns.ReservedPaddingNamespace, appconsts.ShareVersionZero)
	if err != nil {
		panic(err)
	}
	return share
}

// ReservedPaddingShare returns n reserved padding shares.
func ReservedPaddingShares(n int) []Share {
	shares, err := NamespacePaddingShares(appns.ReservedPaddingNamespace, appconsts.ShareVersionZero, n)
	if err != nil {
		panic(err)
	}
	return shares
}

// TailPaddingShare is a share that is used to pad a data square to the desired
// square size. Tail padding shares follow the last blob share in the data
// square.
func TailPaddingShare() Share {
	share, err := NamespacePaddingShare(appns.TailPaddingNamespace, appconsts.ShareVersionZero)
	if err != nil {
		panic(err)
	}
	return share
}

// TailPaddingShares returns n tail padding shares.
func TailPaddingShares(n int) []Share {
	shares, err := NamespacePaddingShares(appns.TailPaddingNamespace, appconsts.ShareVersionZero, n)
	if err != nil {
		panic(err)
	}
	return shares
}

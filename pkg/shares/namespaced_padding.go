package shares

import (
	"bytes"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
)

// NamespacedPaddedShare returns a share that acts as padding. Namespaced
// padding shares follow a blob so that the next blob may start at an index that
// conforms to non-interactive default rules. The ns parameter provided should
// be the namespace of the blob that precedes this padding in the data square.
func NamespacedPaddedShare(ns namespace.ID) Share {
	infoByte, err := NewInfoByte(appconsts.ShareVersionZero, false)
	if err != nil {
		panic(err)
	}
	padding := bytes.Repeat([]byte{0}, appconsts.ShareSize-appconsts.NamespaceSize-appconsts.ShareInfoBytes)

	share := make([]byte, 0, appconsts.ShareSize)
	share = append(share, ns...)
	share = append(share, byte(infoByte))
	share = append(share, padding...)
	return share
}

// NamespacedPaddedShares returns n namespaced padded shares.
func NamespacedPaddedShares(ns namespace.ID, n int) []Share {
	shares := make([]Share, n)
	for i := 0; i < n; i++ {
		shares[i] = NamespacedPaddedShare(ns)
	}
	return shares
}

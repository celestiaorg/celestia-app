package shares

import (
	"bytes"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
)

// TailPaddingShare is a share that is used to pad a data square to the desired
// square size. Tail padding shares follow the last blob share in the data
// square.
func TailPaddingShare() Share {
	infoByte, _ := NewInfoByte(appconsts.ShareVersionZero, false)
	padding := bytes.Repeat([]byte{0}, appconsts.ShareSize-appconsts.NamespaceSize-appconsts.ShareInfoBytes)

	share := make([]byte, 0, appconsts.ShareSize)
	share = append(share, appconsts.TailPaddingNamespaceID...)
	share = append(share, byte(infoByte))
	share = append(share, padding...)
	return share
}

// TailPaddingShares returns n tail padding shares.
func TailPaddingShares(n int) []Share {
	shares := make([]Share, n)
	for i := 0; i < n; i++ {
		shares[i] = TailPaddingShare()
	}
	return shares
}

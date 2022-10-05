package utils

import (
	"bytes"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/nmt/namespace"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func RandomValidNamespace() namespace.ID {
	for {
		ns := tmrand.Bytes(8)
		isReservedNS := bytes.Compare(ns, appconsts.MaxReservedNamespace) <= 0
		isParityNS := bytes.Equal(ns, appconsts.ParitySharesNamespaceID)
		isTailPaddingNS := bytes.Equal(ns, appconsts.TailPaddingNamespaceID)
		if isReservedNS || isParityNS || isTailPaddingNS {
			continue
		}
		return ns
	}
}

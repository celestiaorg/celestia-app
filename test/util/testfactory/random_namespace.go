package testfactory

import (
	"github.com/celestiaorg/celestia-app/pkg/namespace"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func RandomNamespace() namespace.Namespace {
	for {
		id := RandomVerzionZeroID()
		namespace, err := namespace.New(namespace.NamespaceVersionZero, id)
		if err != nil {
			continue
		}
		return namespace
	}
}

func RandomVerzionZeroID() []byte {
	return append(namespace.NamespaceVersionZeroPrefix, tmrand.Bytes(namespace.NamespaceVersionZeroIDSize)...)
}

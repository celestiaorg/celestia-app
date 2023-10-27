package shares

import tmrand "github.com/tendermint/tendermint/libs/rand"

func RandomNamespace() Namespace {
	for {
		id := RandomVerzionZeroID()
		namespace, err := NewNamespace(NamespaceVersionZero, id)
		if err != nil {
			continue
		}
		return namespace
	}
}

func RandomVerzionZeroID() []byte {
	return append(NamespaceVersionZeroPrefix, tmrand.Bytes(NamespaceVersionZeroIDSize)...)
}

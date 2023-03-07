package namespace

import (
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func RandomBlobNamespace() Namespace {
	for {
		randomID := tmrand.Bytes(NamespaceVersionZeroIDSize)
		namespace := MustNewV0(randomID)

		if namespace.IsReserved() || namespace.IsParityShares() || namespace.IsTailPadding() {
			continue
		}

		return namespace
	}
}

func RandomBlobNamespaces(count int) (namespaces []Namespace) {
	for i := 0; i < count; i++ {
		namespaces = append(namespaces, RandomBlobNamespace())
	}
	return namespaces
}

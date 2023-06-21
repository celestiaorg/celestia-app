package namespace

import (
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func RandomBlobNamespaceID(rand *tmrand.Rand) []byte {
	return tmrand.Bytes(NamespaceVersionZeroIDSize)
}

func RandomBlobNamespace(rand *tmrand.Rand) Namespace {
	for {
		id := RandomBlobNamespaceID(rand)
		namespace := MustNewV0(id)
		err := namespace.ValidateBlobNamespace()
		if err != nil {
			continue
		}
		return namespace
	}
}

func RandomBlobNamespaces(rand *tmrand.Rand, count int) (namespaces []Namespace) {
	for i := 0; i < count; i++ {
		namespaces = append(namespaces, RandomBlobNamespace(rand))
	}
	return namespaces
}

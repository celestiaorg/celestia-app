package namespace

import (
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func RandomBlobNamespaceID() []byte {
	return tmrand.Bytes(NamespaceVersionZeroIDSize)
}

func RandomBlobNamespace() Namespace {
	for {
		id := RandomBlobNamespaceID()
		namespace := MustNewV0(id)
		err := namespace.ValidateBlobNamespace()
		if err != nil {
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

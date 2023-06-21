package namespace

import (
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func RandomBlobNamespaceID() []byte {
	return RandomBlobNamespaceIDWithPRG(tmrand.NewRand())
}

func RandomBlobNamespaceIDWithPRG(rand *tmrand.Rand) []byte {
	return rand.Bytes(NamespaceVersionZeroIDSize)
}

func RandomBlobNamespace() Namespace {
	return RandomBlobNamespaceWithPRG(tmrand.NewRand())
}

func RandomBlobNamespaceWithPRG(rand *tmrand.Rand) Namespace {
	for {
		id := RandomBlobNamespaceIDWithPRG(rand)
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
		namespaces = append(namespaces, RandomBlobNamespaceWithPRG(rand))
	}
	return namespaces
}

package namespace

import (
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func RandomBlobNamespaceID() []byte {
	return RandomBlobNamespaceIDWithPRG(tmrand.NewRand())
}

// RandomBlobNamespaceIDWithPRG returns a random blob namespace ID using the supplied Pseudo-Random number Generator (PRG).
func RandomBlobNamespaceIDWithPRG(prg *tmrand.Rand) []byte {
	return prg.Bytes(NamespaceVersionZeroIDSize)
}

func RandomBlobNamespace() Namespace {
	return RandomBlobNamespaceWithPRG(tmrand.NewRand())
}

// RandomBlobNamespaceWithPRG generates and returns a random blob namespace using the supplied Pseudo-Random number Generator (PRG).
func RandomBlobNamespaceWithPRG(prg *tmrand.Rand) Namespace {
	for {
		id := RandomBlobNamespaceIDWithPRG(prg)
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

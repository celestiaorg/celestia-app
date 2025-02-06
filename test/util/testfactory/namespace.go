package testfactory

import (
	"slices"

	"github.com/celestiaorg/go-square/v2/share"
	tmrand "github.com/cometbft/cometbft/libs/rand"
)

// RandomBlobNamespaceIDWithPRG returns a random blob namespace ID using the supplied Pseudo-Random number Generator (PRG).
func RandomBlobNamespaceIDWithPRG(prg *tmrand.Rand) []byte {
	return prg.Bytes(share.NamespaceVersionZeroIDSize)
}

func RandomBlobNamespace() share.Namespace {
	return RandomBlobNamespaceWithPRG(tmrand.NewRand())
}

// RandomBlobNamespaceWithPRG generates and returns a random blob namespace using the supplied Pseudo-Random number Generator (PRG).
func RandomBlobNamespaceWithPRG(prg *tmrand.Rand) share.Namespace {
	for {
		id := RandomBlobNamespaceIDWithPRG(prg)
		namespace := share.MustNewV0Namespace(id)
		if isBlobNamespace(namespace) {
			return namespace
		}
	}
}

func RandomBlobNamespaces(rand *tmrand.Rand, count int) (namespaces []share.Namespace) {
	for i := 0; i < count; i++ {
		namespaces = append(namespaces, RandomBlobNamespaceWithPRG(rand))
	}
	return namespaces
}

// isBlobNamespace returns a true if this namespace is a valid user-specifiable
// blob namespace.
func isBlobNamespace(namespace share.Namespace) bool {
	if namespace.IsReserved() {
		return false
	}

	if !slices.Contains(share.SupportedBlobNamespaceVersions, namespace.Version()) {
		return false
	}

	return true
}

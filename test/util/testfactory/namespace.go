package testfactory

import (
	"math/rand"
	"slices"

	"github.com/celestiaorg/go-square/v2/share"

	"github.com/celestiaorg/celestia-app/v4/test/util/random"
)

// RandomBlobNamespaceIDWithPRG returns a random blob namespace ID using the supplied Pseudo-Random number Generator (PRG).
func RandomBlobNamespaceIDWithPRG(r *rand.Rand) []byte {
	return random.BytesR(r, share.NamespaceVersionZeroIDSize)
}

func RandomBlobNamespace() share.Namespace {
	return RandomBlobNamespaceWithPRG(random.New())
}

// RandomBlobNamespaceWithPRG generates and returns a random blob namespace using the supplied Pseudo-Random number Generator (PRG).
func RandomBlobNamespaceWithPRG(rand *rand.Rand) share.Namespace {
	for {
		id := RandomBlobNamespaceIDWithPRG(rand)
		namespace := share.MustNewV0Namespace(id)
		if isBlobNamespace(namespace) {
			return namespace
		}
	}
}

func RandomBlobNamespaces(rand *rand.Rand, count int) (namespaces []share.Namespace) {
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

package testfactory

import (
	"slices"

	ns "github.com/celestiaorg/go-square/namespace"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

// RandomBlobNamespaceIDWithPRG returns a random blob namespace ID using the supplied Pseudo-Random number Generator (PRG).
func RandomBlobNamespaceIDWithPRG(prg *tmrand.Rand) []byte {
	return prg.Bytes(ns.NamespaceVersionZeroIDSize)
}

func RandomBlobNamespace() ns.Namespace {
	return RandomBlobNamespaceWithPRG(tmrand.NewRand())
}

// RandomBlobNamespaceWithPRG generates and returns a random blob namespace using the supplied Pseudo-Random number Generator (PRG).
func RandomBlobNamespaceWithPRG(prg *tmrand.Rand) ns.Namespace {
	for {
		id := RandomBlobNamespaceIDWithPRG(prg)
		namespace := ns.MustNewV0(id)
		if isBlobNamespace(namespace) {
			return namespace
		}
	}
}

func RandomBlobNamespaces(rand *tmrand.Rand, count int) (namespaces []ns.Namespace) {
	for i := 0; i < count; i++ {
		namespaces = append(namespaces, RandomBlobNamespaceWithPRG(rand))
	}
	return namespaces
}

// isBlobNamespace returns an true if this namespace is a valid user-specifiable
// blob namespace.
func isBlobNamespace(namespace ns.Namespace) bool {
	if namespace.IsReserved() {
		return false
	}

	if !slices.Contains(ns.SupportedBlobNamespaceVersions, namespace.Version) {
		return false
	}

	return true
}

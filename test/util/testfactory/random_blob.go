package testfactory

import (
	"github.com/celestiaorg/celestia-app/pkg/namespace"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"golang.org/x/exp/slices"
)

func RandomBlobNamespaceID() []byte {
	return RandomBlobNamespaceIDWithPRG(tmrand.NewRand())
}

// RandomBlobNamespaceIDWithPRG returns a random blob namespace ID using the supplied Pseudo-Random number Generator (PRG).
func RandomBlobNamespaceIDWithPRG(prg *tmrand.Rand) []byte {
	return prg.Bytes(namespace.NamespaceVersionZeroIDSize)
}

func RandomBlobNamespace() namespace.Namespace {
	return RandomBlobNamespaceWithPRG(tmrand.NewRand())
}

// RandomBlobNamespaceWithPRG generates and returns a random blob namespace using the supplied Pseudo-Random number Generator (PRG).
func RandomBlobNamespaceWithPRG(prg *tmrand.Rand) namespace.Namespace {
	for {
		id := RandomBlobNamespaceIDWithPRG(prg)
		namespace := namespace.MustNewV0(id)
		if isBlobNamespace(namespace) {
			return namespace
		}
	}
}

func RandomBlobNamespaces(rand *tmrand.Rand, count int) (namespaces []namespace.Namespace) {
	for i := 0; i < count; i++ {
		namespaces = append(namespaces, RandomBlobNamespaceWithPRG(rand))
	}
	return namespaces
}

// isBlobNamespace returns an true if this namespace is a valid user-specifiable
// blob namespace.
func isBlobNamespace(ns namespace.Namespace) bool {
	if ns.IsReserved() {
		return false
	}

	if !slices.Contains(namespace.SupportedBlobNamespaceVersions, ns.Version) {
		return false
	}

	return true
}

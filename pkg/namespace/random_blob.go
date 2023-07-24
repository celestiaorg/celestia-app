package namespace

import (
	"fmt"

	tmrand "github.com/tendermint/tendermint/libs/rand"
	"golang.org/x/exp/slices"
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
		err := validateBlobNamespace(namespace)
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

// validateBlobNamespace returns an error if this namespace is not a valid
// user-specifiable blob namespace.
func validateBlobNamespace(ns Namespace) error {
	if ns.IsReserved() {
		return fmt.Errorf("invalid blob namespace: %v cannot use a reserved namespace", ns.Bytes())
	}

	if ns.IsParityShares() {
		return fmt.Errorf("invalid blob namespace: %v cannot use parity shares namespace", ns.Bytes())
	}

	if ns.IsTailPadding() {
		return fmt.Errorf("invalid blob namespace: %v cannot use tail padding namespace", ns.Bytes())
	}

	if !slices.Contains(SupportedBlobNamespaceVersions, ns.Version) {
		return fmt.Errorf("invalid blob namespace version: %v", ns.Version)
	}

	return nil
}

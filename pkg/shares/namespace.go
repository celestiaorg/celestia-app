package shares

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/namespace"
)

// GetShareRangeForNamespace returns all shares that belong to a given
// namespace. It will return an empty range if the namespace could not be
// found. This assumes that the slice of shares are lexicographically
// sorted by namespace. Ranges here are always end exlusive.
func GetShareRangeForNamespace(shares []Share, ns namespace.Namespace) (Range, error) {
	if len(shares) == 0 {
		return EmptyRange(), nil
	}
	n0, err := shares[0].Namespace()
	if err != nil {
		return EmptyRange(), err
	}
	if ns.IsLessThan(n0) {
		return EmptyRange(), nil
	}
	n1, err := shares[len(shares)-1].Namespace()
	if err != nil {
		return EmptyRange(), err
	}
	if ns.IsGreaterThan(n1) {
		return EmptyRange(), nil
	}

	start := -1
	for i, share := range shares {
		shareNS, err := share.Namespace()
		if err != nil {
			return EmptyRange(), fmt.Errorf("failed to get namespace from share %d: %w", i, err)
		}
		if shareNS.IsGreaterThan(ns) && start != -1 {
			return Range{start, i}, nil
		}
		if ns.Equals(shareNS) && start == -1 {
			start = i
		}
	}
	if start == -1 {
		return EmptyRange(), nil
	}
	return Range{start, len(shares)}, nil
}

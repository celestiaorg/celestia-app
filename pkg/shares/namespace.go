package shares

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/namespace"
)

// GetSharesByNamespace returns all shares that belong to a given namespace.
// It will return an empty range if the namespace could not be found.
// This assumes that the slice of shares are lexicographically sorted by 
// namespace.
func GetShareRangeForNamespace(shares []Share, ns namespace.Namespace) (ShareRange, error) {
	if len(shares) == 0 {
		return ShareRange{}, nil
	}
	n0, err := shares[0].Namespace()
	if err != nil {
		return ShareRange{}, err
	}
	if ns.IsLessThan(n0) {
		return ShareRange{}, nil
	}
	n1, err := shares[len(shares)-1].Namespace()
	if err != nil {
		return ShareRange{}, err
	}
	if ns.IsGreaterThan(n1) {
		return ShareRange{}, nil
	}

	var start = -1
	for i, share := range shares {
		shareNS, err := share.Namespace()
		if err != nil {
			return ShareRange{}, fmt.Errorf("failed to get namespace from share %d: %w", i, err)
		}
		if ns.Equals(shareNS) && start == -1 {
			start = i
		}
		if shareNS.IsGreaterThan(ns) && start != -1 {
			return ShareRange{start, i-1}, nil
		}
	}
	if start == -1 {
		return ShareRange{}, nil
	}
	return ShareRange{start, len(shares)-1}, nil
}

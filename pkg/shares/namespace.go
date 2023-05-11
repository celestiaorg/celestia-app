package shares

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/pkg/namespace"
)

// GetSharesByNamespace returns all shares that belong to a given namespace. It assumes
// the slice of shares are lexicographically sorted by namespace.
func GetShareRangeByNamespace(shares []Share, ns namespace.Namespace) (ShareRange, error) {
	if len(shares) == 0 {
		return ShareRange{}, fmt.Errorf("no shares provided")
	}
	n0, err := shares[0].Namespace()
	if err != nil {
		return ShareRange{}, err
	}
	if ns.IsLessThan(n0) {
		return ShareRange{}, fmt.Errorf("namespace %v is less than the first share's namespace %v", ns, n0)
	}
	n1, err := shares[len(shares)-1].Namespace()
	if err != nil {
		return ShareRange{}, err
	}
	if ns.IsGreaterThan(n1) {
		return ShareRange{}, fmt.Errorf("namespace %v is greater than the last share's namespace %v", ns, n1)
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
			return ShareRange{start, i - 1}, nil
		}
	}
	if start == -1 {
		return ShareRange{}, fmt.Errorf("no shares found for namespace %v", ns)
	}
	return ShareRange{start, len(shares) - 1}, nil
}

package appconsts_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/pkg/consts"
)

// TestCelestiaCoreHashFunctionMatches is a feeble attempt to ensure that the
// same hash function is used in core and appconsts. The function is not
// directly imported from core because the appconsts module should be usable
// without having to import core.
func TestCelestiaCoreHashFunctionMatches(t *testing.T) {
	coreHasher := consts.NewBaseHashFunc()
	appHasher := appconsts.NewBaseHashFunc()

	// Test that the hash functions match for a few different inputs
	for _, input := range []string{"", "a", "test", "test string"} {
		coreHash := coreHasher.Sum([]byte(input))
		appHash := appHasher.Sum([]byte(input))
		require.Equal(t, coreHash, appHash)
	}
}

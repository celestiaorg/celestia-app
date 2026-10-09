package wrapper_test

import (
	"math/rand"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/da"
	"github.com/celestiaorg/celestia-app/v10/pkg/wrapper"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/stretchr/testify/require"
)

// TestComputeRootsMatchesUntiledRoots is the correctness gate for sharing NMT
// leaf hashes between the row and column trees.
//
// The tiled path computes the row and column roots - and therefore the data
// root every validator compares against the proposer's - a different way. A
// single square where it disagrees with the established path would fork the
// network, so this asserts byte-identical roots across every square size the
// guard admits, over random, repeated and adversarial namespace layouts.
func TestComputeRootsMatchesUntiledRoots(t *testing.T) {
	pool, err := wrapper.DefaultPreallocatedTreePool(uint(appconsts.SquareSizeUpperBound))
	require.NoError(t, err)

	for _, n := range []int{1, 2, 4, 8, 16, 32, 64} {
		for _, layout := range []string{"random", "repeated", "max namespace", "identical"} {
			t.Run(layoutName(n, layout), func(t *testing.T) {
				data := squareOf(t, n, layout)

				// the established path: extend, then build the header
				eds, err := da.ExtendSharesWithTreePool(data, pool)
				require.NoError(t, err)
				want, err := da.NewDataAvailabilityHeader(eds)
				require.NoError(t, err)
				expectedSize := uint64(eds.Width()) / 2

				size, rows, columns, err := pool.ComputeRoots(data, expectedSize)
				require.NoError(t, err)
				require.Equal(t, expectedSize, size)
				require.Equal(t, want.RowRoots, rows, "row roots differ at n=%d", n)
				require.Equal(t, want.ColumnRoots, columns, "column roots differ at n=%d", n)

				got := da.DataAvailabilityHeader{RowRoots: rows, ColumnRoots: columns}
				require.Equal(t, want.Hash(), got.Hash(), "data root differs at n=%d", n)
			})
		}
	}
}

// TestComputeRootsRejectsNonSquareInput pins the guard: anything the tiled
// path was not derived for must error or fall back, never produce a root of
// its own.
func TestComputeRootsRejectsNonSquareInput(t *testing.T) {
	pool, err := wrapper.DefaultPreallocatedTreePool(uint(appconsts.SquareSizeUpperBound))
	require.NoError(t, err)

	_, _, _, err = pool.ComputeRoots(nil, 0)
	require.Error(t, err)

	// not a power of two
	_, _, _, err = pool.ComputeRoots(make([][]byte, 3), 2)
	require.Error(t, err)
}

func layoutName(n int, layout string) string {
	return layout + "/n=" + itoa(n)
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var out []byte
	for n > 0 {
		out = append([]byte{byte('0' + n%10)}, out...)
		n /= 10
	}
	return string(out)
}

// squareOf builds n*n namespace-ordered shares in the requested layout.
func squareOf(t *testing.T, n int, layout string) [][]byte {
	t.Helper()
	rng := rand.New(rand.NewSource(int64(n) * 7))
	count := n * n
	namespaces := make([][]byte, count)

	switch layout {
	case "identical":
		ns := namespaceFor(t, 1)
		for i := range namespaces {
			namespaces[i] = ns
		}
	case "repeated":
		for i := range namespaces {
			namespaces[i] = namespaceFor(t, i/3+1)
		}
	case "max namespace":
		for i := range namespaces {
			if i >= count/2 {
				namespaces[i] = share.ParitySharesNamespace.Bytes()
				continue
			}
			namespaces[i] = namespaceFor(t, i+1)
		}
	default:
		for i := range namespaces {
			namespaces[i] = namespaceFor(t, i+1)
		}
	}

	shares := make([][]byte, count)
	for i := range shares {
		s := make([]byte, share.ShareSize)
		copy(s, namespaces[i])
		_, err := rng.Read(s[share.NamespaceSize:])
		require.NoError(t, err)
		shares[i] = s
	}
	return shares
}

// namespaceFor builds an ordered namespace: the shares of a square must be
// sorted by namespace, so these increase with i.
func namespaceFor(t *testing.T, i int) []byte {
	t.Helper()
	ns := make([]byte, share.NamespaceSize)
	ns[share.NamespaceSize-4] = byte(i >> 24)
	ns[share.NamespaceSize-3] = byte(i >> 16)
	ns[share.NamespaceSize-2] = byte(i >> 8)
	ns[share.NamespaceSize-1] = byte(i)
	return ns
}

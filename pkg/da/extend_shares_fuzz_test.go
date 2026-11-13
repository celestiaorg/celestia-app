package da

import (
	"bytes"
	"fmt"
	"slices"
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/wrapper"
	sh "github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	"github.com/celestiaorg/rsmt2d"
	"github.com/stretchr/testify/require"
)

// maxFuzzShares bounds the original square to 8x8 so executions stay fast.
const maxFuzzShares = 8 * 8

var (
	fuzzTreePoolOnce sync.Once
	fuzzTreePool     *wrapper.TreePool
)

// sharedFuzzTreePool returns one small pool shared by every fuzz input, like
// the long-lived pool the app passes to ExtendSharesWithTreePool. Starting at
// square size 1 forces trees to be resized, and failed inputs abandon trees,
// so later inputs also exercise reuse after both.
func sharedFuzzTreePool(tb testing.TB) *wrapper.TreePool {
	tb.Helper()
	fuzzTreePoolOnce.Do(func() {
		pool, err := wrapper.NewTreePool(1, 4)
		if err != nil {
			panic(err)
		}
		fuzzTreePool = pool
	})
	return fuzzTreePool
}

// fuzzShares expands data into count shares of sh.ShareSize bytes, repeating
// data cyclically. Optionally the shares are sorted by namespace so that the
// namespace merkle trees accept them.
func fuzzShares(count int, sortByNamespace bool, data []byte) [][]byte {
	shares := make([][]byte, count)
	for i := range shares {
		s := make([]byte, sh.ShareSize)
		for j := range s {
			if len(data) == 0 {
				s[j] = byte(i)
				continue
			}
			s[j] = data[(i*sh.ShareSize+j)%len(data)]
		}
		shares[i] = s
	}
	if sortByNamespace {
		slices.SortStableFunc(shares, func(a, b []byte) int {
			return bytes.Compare(a[:sh.NamespaceSize], b[:sh.NamespaceSize])
		})
	}
	return shares
}

// extendSharesSeeds returns original squares built the way PrepareProposal
// builds them, plus hand-made squares that place the parity and max
// namespaces in the original data.
func extendSharesSeeds(tb testing.TB) [][]byte {
	tb.Helper()
	ns := sh.MustNewV0Namespace(bytes.Repeat([]byte{7}, sh.NamespaceVersionZeroIDSize))
	blob, err := sh.NewV0Blob(ns, bytes.Repeat([]byte{0xAB}, 3000))
	require.NoError(tb, err)
	btx, err := blobtx.MarshalBlobTx(bytes.Repeat([]byte{0x02}, 300), blob)
	require.NoError(tb, err)

	seeds := make([][]byte, 0, 6)
	for _, txs := range [][][]byte{
		{bytes.Repeat([]byte{0x01}, 200)},
		{bytes.Repeat([]byte{0x01}, 200), btx},
		{bytes.Repeat([]byte{0x01}, sh.AvailableBytesFromCompactShares(15))},
	} {
		shares, err := constructShares(txs, appconsts.Version, 8)
		require.NoError(tb, err)
		seeds = append(seeds, bytes.Join(shares, nil))
	}
	seeds = append(seeds,
		bytes.Join(generateShares(4), nil),
		bytes.Repeat(sh.ParitySharesNamespace.Bytes(), 4),
		bytes.Join([][]byte{sh.TxNamespace.Bytes(), sh.ParitySharesNamespace.Bytes()}, nil),
	)
	return seeds
}

// checkExtendSharesParity extends shares with the pooled buffered trees used
// by PrepareProposal and ProcessProposal and with the NMT wrapper used by
// proofs and DA nodes, and requires both to agree on every share, root, error
// and the data root.
func checkExtendSharesParity(t *testing.T, shares [][]byte) {
	t.Helper()
	pool := sharedFuzzTreePool(t)

	plain, plainErr := ExtendShares(shares)
	pooled, pooledErr := ExtendSharesWithTreePool(shares, pool)
	// The two functions validate the share count with different messages, so
	// only agreement on whether extension fails is required here.
	require.Equal(t, plainErr == nil, pooledErr == nil, "extension disagrees: plain=%v pooled=%v", plainErr, pooledErr)
	if plainErr != nil {
		return
	}
	require.Equal(t, plain.Width(), pooled.Width())
	require.Equal(t, plain.Flattened(), pooled.Flattened(), "erasure coded shares differ")

	width := plain.Width()
	squareSize := width / 2
	plainTree := wrapper.NewConstructor(uint64(squareSize))
	pooledTree := pool.NewConstructor(squareSize)

	// Roots are computed concurrently inside rsmt2d, which returns whichever
	// error comes first, so compare each axis sequentially for exact errors.
	allOK := true
	rowRoots := make([][]byte, width)
	colRoots := make([][]byte, width)
	for _, axis := range []rsmt2d.Axis{rsmt2d.Row, rsmt2d.Col} {
		for i := range width {
			cells := plain.Row(i)
			if axis == rsmt2d.Col {
				cells = plain.Col(i)
			}
			want, wantErr := axisRoot(plainTree(axis, i), cells)
			got, gotErr := axisRoot(pooledTree(axis, i), cells)
			require.Equal(t, errString(wantErr), errString(gotErr), "%s %d error", axis, i)
			require.Equal(t, want, got, "%s %d root", axis, i)
			if wantErr != nil {
				allOK = false
				continue
			}
			if axis == rsmt2d.Row {
				rowRoots[i] = want
			} else {
				colRoots[i] = want
			}
		}
	}

	plainDAH, plainErr := NewDataAvailabilityHeader(plain)
	pooledDAH, pooledErr := NewDataAvailabilityHeader(pooled)
	require.Equal(t, allOK, plainErr == nil, "plain header error %v does not match per-axis results", plainErr)
	require.Equal(t, allOK, pooledErr == nil, "pooled header error %v does not match per-axis results", pooledErr)
	if !allOK {
		return
	}
	require.Equal(t, rowRoots, plainDAH.RowRoots)
	require.Equal(t, colRoots, plainDAH.ColumnRoots)
	require.Equal(t, plainDAH.RowRoots, pooledDAH.RowRoots, "row roots differ")
	require.Equal(t, plainDAH.ColumnRoots, pooledDAH.ColumnRoots, "column roots differ")
	require.Equal(t, plainDAH.Hash(), pooledDAH.Hash(), "data roots differ")
}

func axisRoot(tree rsmt2d.Tree, cells [][]byte) ([]byte, error) {
	for _, cell := range cells {
		if err := tree.Push(cell); err != nil {
			return nil, err
		}
	}
	return tree.Root()
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}

func TestExtendSharesParitySeeds(t *testing.T) {
	for i, data := range extendSharesSeeds(t) {
		t.Run(fmt.Sprint(i), func(t *testing.T) {
			count := len(data) / sh.ShareSize
			if count == 0 {
				count = 4
			}
			checkExtendSharesParity(t, fuzzShares(count, false, data))
		})
	}
}

// FuzzExtendSharesParity checks that ExtendSharesWithTreePool, which
// PrepareProposal and ProcessProposal use to compute the data root, agrees
// with ExtendShares and the NMT wrapper, which proofs and DA nodes use, for
// proposer-controlled original squares of up to 8x8 shares. Inputs include
// non-square share counts, out-of-order namespaces and arbitrary share bytes.
func FuzzExtendSharesParity(f *testing.F) {
	for _, data := range extendSharesSeeds(f) {
		count := uint8(len(data) / sh.ShareSize)
		if count == 0 {
			count = 4
		}
		f.Add(count, false, data)
		f.Add(count, true, data)
	}
	f.Add(uint8(2), false, []byte{})
	f.Add(uint8(16), true, []byte{0xFF, 0x00, 0x01})
	f.Fuzz(func(t *testing.T, count uint8, sortByNamespace bool, data []byte) {
		if int(count) > maxFuzzShares || len(data) > maxFuzzShares*sh.ShareSize {
			return
		}
		checkExtendSharesParity(t, fuzzShares(int(count), sortByNamespace, data))
	})
}

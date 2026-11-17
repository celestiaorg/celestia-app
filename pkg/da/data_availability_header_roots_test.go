package da

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestValidateBasicRejectsBadRootLength(t *testing.T) {
	for name, mutate := range map[string]func(*DataAvailabilityHeader){
		"short row root":    func(d *DataAvailabilityHeader) { d.RowRoots[0] = d.RowRoots[0][:1] },
		"short column root": func(d *DataAvailabilityHeader) { d.ColumnRoots[1] = d.ColumnRoots[1][:89] },
		"long row root":     func(d *DataAvailabilityHeader) { d.RowRoots[1] = append(bytes.Clone(d.RowRoots[1]), 0) },
		"empty column root": func(d *DataAvailabilityHeader) { d.ColumnRoots[0] = nil },
	} {
		t.Run(name, func(t *testing.T) {
			dah := MinDataAvailabilityHeader()
			dah.RowRoots = cloneRoots(dah.RowRoots)
			dah.ColumnRoots = cloneRoots(dah.ColumnRoots)
			mutate(&dah)
			dah.hash = nil
			err := dah.ValidateBasic()
			require.Error(t, err)
			require.Contains(t, err.Error(), "root")
			require.Contains(t, err.Error(), "90 bytes")
		})
	}
}

func TestValidateBasicRejectsNonPowerOfTwoRootCount(t *testing.T) {
	root := MinDataAvailabilityHeader().RowRoots[0]
	for _, n := range []int{6, 10, 12, 18, 1022} {
		roots := make([][]byte, n)
		for i := range roots {
			roots[i] = bytes.Clone(root)
		}
		dah := DataAvailabilityHeader{RowRoots: roots, ColumnRoots: cloneRoots(roots)}
		err := dah.ValidateBasic()
		require.Error(t, err, "n=%d", n)
		require.Contains(t, err.Error(), "power of two", "n=%d", n)
	}
}

func cloneRoots(roots [][]byte) [][]byte {
	out := make([][]byte, len(roots))
	for i, r := range roots {
		out[i] = bytes.Clone(r)
	}
	return out
}

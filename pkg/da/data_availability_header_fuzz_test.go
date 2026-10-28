package da

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	daproto "github.com/celestiaorg/celestia-app/v11/proto/celestia/core/v1/da"
	"github.com/stretchr/testify/require"
)

// dahSeeds returns the minimum header and the header of a small square.
func dahSeeds(tb testing.TB) [][]byte {
	tb.Helper()
	minDAH := MinDataAvailabilityHeader()
	eds, err := ExtendShares(generateShares(4 * 4))
	require.NoError(tb, err)
	small, err := NewDataAvailabilityHeader(eds)
	require.NoError(tb, err)
	seeds := make([][]byte, 0, 2)
	for _, dah := range []DataAvailabilityHeader{minDAH, small} {
		pb, err := dah.ToProto()
		require.NoError(tb, err)
		raw, err := pb.Marshal()
		require.NoError(tb, err)
		seeds = append(seeds, raw)
	}
	return seeds
}

// checkDAH decodes raw as a data availability header the way a header
// received from the network is decoded, and checks that a header which
// validates survives a proto round trip with the same hash.
func checkDAH(t *testing.T, raw []byte) {
	t.Helper()
	var pb daproto.DataAvailabilityHeader
	if err := pb.Unmarshal(raw); err != nil {
		return
	}
	dah, err := DataAvailabilityHeaderFromProto(&pb)
	if err != nil {
		return
	}
	require.NoError(t, dah.ValidateBasic())
	require.False(t, dah.IsZero())
	require.Len(t, dah.Hash(), 32)
	require.GreaterOrEqual(t, dah.SquareSize(), appconsts.MinSquareSize)
	require.LessOrEqual(t, dah.SquareSize(), appconsts.SquareSizeUpperBound)
	again, err := dah.ToProto()
	require.NoError(t, err)
	back, err := DataAvailabilityHeaderFromProto(again)
	require.NoError(t, err)
	require.True(t, dah.Equals(back), "header changed across a proto round trip")
	require.Equal(t, dah.Hash(), back.Hash())
}

func TestDAHSeeds(t *testing.T) {
	for i, raw := range dahSeeds(t) {
		var pb daproto.DataAvailabilityHeader
		require.NoError(t, pb.Unmarshal(raw))
		_, err := DataAvailabilityHeaderFromProto(&pb)
		require.NoError(t, err, "seed %d must decode", i)
		checkDAH(t, raw)
	}
}

// FuzzDataAvailabilityHeaderFromProto decodes mutated header bytes and runs
// the stateless validation and hashing a node applies to a received header.
func FuzzDataAvailabilityHeaderFromProto(f *testing.F) {
	for _, raw := range dahSeeds(f) {
		f.Add(raw)
	}
	f.Add([]byte{})
	f.Fuzz(func(t *testing.T, raw []byte) {
		// The largest valid header holds 2*512 row and column roots of 90 bytes.
		if len(raw) > 1<<20 {
			return
		}
		checkDAH(t, raw)
	})
}

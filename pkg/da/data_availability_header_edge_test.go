package da

import (
	"bytes"
	"testing"

	daproto "github.com/celestiaorg/celestia-app/v11/proto/celestia/core/v1/da"
	"github.com/stretchr/testify/require"
)

// TestHashCoversAllColumnRoots checks that headers with different column roots
// never hash equal, even when they fail ValidateBasic.
func TestHashCoversAllColumnRoots(t *testing.T) {
	a := MinDataAvailabilityHeader()
	b := DataAvailabilityHeader{
		RowRoots:    a.RowRoots,
		ColumnRoots: append(append([][]byte{}, a.ColumnRoots...), bytes.Repeat([]byte{9}, rootSize)),
	}
	require.NotEqual(t, a.Hash(), b.Hash())
}

func TestSquareSizeNilHeader(t *testing.T) {
	var dah *DataAvailabilityHeader
	require.NotPanics(t, func() {
		require.Zero(t, dah.SquareSize())
	})
}

func TestDataAvailabilityHeaderFromProtoReturnsNilOnError(t *testing.T) {
	root := MinDataAvailabilityHeader().RowRoots[0]
	dah, err := DataAvailabilityHeaderFromProto(&daproto.DataAvailabilityHeader{
		RowRoots:    [][]byte{root},
		ColumnRoots: [][]byte{root},
	})
	require.Error(t, err)
	require.Nil(t, dah)
}

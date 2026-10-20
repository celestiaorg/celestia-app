package app

import (
	"bytes"
	"sync/atomic"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/da"
	"github.com/celestiaorg/celestia-app/v10/pkg/wrapper"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	squarev4 "github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/stretchr/testify/require"
)

// TestBuildProposalSquareSize checks the root ProcessProposal compares
// against the established extend-then-header path, and that a wrong proposed
// size returns the real size and no root, without an error.
func TestBuildProposalSquareSize(t *testing.T) {
	pool, err := wrapper.DefaultPreallocatedTreePool(uint(appconsts.SquareSizeUpperBound))
	require.NoError(t, err)
	app := &App{treePool: pool}

	txs := make([][]byte, 40)
	for i := range txs {
		txs[i] = bytes.Repeat([]byte{byte(i + 1)}, 2000)
	}
	maxSquareSize := appconsts.SquareSizeUpperBound

	classified, err := fibretypes.ClassifyTxsDecoded(txs, nil, nil)
	require.NoError(t, err)
	dataSquare, err := squarev4.Construct(classified, maxSquareSize, appconsts.SubtreeRootThreshold)
	require.NoError(t, err)
	eds, err := da.ExtendSharesWithTreePool(share.ToBytes(dataSquare), pool)
	require.NoError(t, err)
	want, err := da.NewDataAvailabilityHeader(eds)
	require.NoError(t, err)
	size := uint64(eds.Width()) / 2
	require.GreaterOrEqual(t, size, uint64(8), "the square must reach the tiled path")

	var cancel atomic.Bool
	got := app.buildProposalSquare(txs, nil, nil, maxSquareSize, size, &cancel)
	require.NoError(t, got.err)
	require.Equal(t, size, got.size)
	require.Equal(t, want.Hash(), got.root)

	for _, wrong := range []uint64{size / 2, size * 2, size + 1<<63} {
		got := app.buildProposalSquare(txs, nil, nil, maxSquareSize, wrong, &cancel)
		require.NoError(t, got.err)
		require.Equal(t, size, got.size)
		require.Nil(t, got.root)
	}
}

// TestBuildProposalSquareCancelledIsAnError pins that a cancelled build reports
// an error. Its result is otherwise zero, and a zero size and root would match
// a proposal claiming SquareSize 0 with an empty data root.
func TestBuildProposalSquareCancelledIsAnError(t *testing.T) {
	var cancel atomic.Bool
	cancel.Store(true)
	result := (&App{}).buildProposalSquare(nil, nil, nil, 64, 0, &cancel)
	require.ErrorIs(t, result.err, errProposalSquareCancelled)
}

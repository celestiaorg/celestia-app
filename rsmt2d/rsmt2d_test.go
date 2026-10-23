package rsmt2d_test

import (
	"bytes"
	"errors"
	"testing"

	"github.com/celestiaorg/celestia-app/rsmt2d"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shareSize is the size of each share (in bytes) used for testing.
const shareSize = 512

func TestEdsRepairRoundtripSimple(t *testing.T) {
	tests := []struct {
		name  string
		codec rsmt2d.Codec
	}{
		{"leopard", rsmt2d.NewLeoRSCodec()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ones := bytes.Repeat([]byte{1}, shareSize)
			twos := bytes.Repeat([]byte{2}, shareSize)
			threes := bytes.Repeat([]byte{3}, shareSize)
			fours := bytes.Repeat([]byte{4}, shareSize)

			// Compute parity shares
			eds, err := rsmt2d.ComputeExtendedDataSquare(
				[][]byte{
					ones, twos,
					threes, fours,
				},
				tt.codec,
				rsmt2d.NewDefaultTree,
			)
			if err != nil {
				t.Errorf("ComputeExtendedDataSquare failed: %v", err)
			}

			rowRoots, err := eds.RowRoots()
			assert.NoError(t, err)

			colRoots, err := eds.ColRoots()
			assert.NoError(t, err)

			flattened := eds.Flattened()

			// Delete some shares, just enough so that repairing is possible.
			flattened[0], flattened[2], flattened[3] = nil, nil, nil
			flattened[4], flattened[5], flattened[6], flattened[7] = nil, nil, nil, nil
			flattened[8], flattened[9], flattened[10] = nil, nil, nil
			flattened[12], flattened[13] = nil, nil

			// Re-import the data square.
			eds, err = rsmt2d.ImportExtendedDataSquare(flattened, tt.codec, rsmt2d.NewDefaultTree)
			if err != nil {
				t.Errorf("ImportExtendedDataSquare failed: %v", err)
			}

			// Repair square.
			err = eds.Repair(
				rowRoots,
				colRoots,
			)
			if err != nil {
				// err contains information to construct a fraud proof
				// See extendeddatacrossword_test.go
				t.Errorf("RepairExtendedDataSquare failed: %v", err)
			}
		})
	}
}

func TestEdsRepairTwice(t *testing.T) {
	tests := []struct {
		name  string
		codec rsmt2d.Codec
	}{
		{"leopard", rsmt2d.NewLeoRSCodec()},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ones := bytes.Repeat([]byte{1}, shareSize)
			twos := bytes.Repeat([]byte{2}, shareSize)
			threes := bytes.Repeat([]byte{3}, shareSize)
			fours := bytes.Repeat([]byte{4}, shareSize)

			// Compute parity shares
			eds, err := rsmt2d.ComputeExtendedDataSquare(
				[][]byte{
					ones, twos,
					threes, fours,
				},
				tt.codec,
				rsmt2d.NewDefaultTree,
			)
			if err != nil {
				t.Errorf("ComputeExtendedDataSquare failed: %v", err)
			}

			rowRoots, err := eds.RowRoots()
			assert.NoError(t, err)

			colRoots, err := eds.ColRoots()
			assert.NoError(t, err)

			flattened := eds.Flattened()

			// Delete some shares, just enough so that repairing is possible, then remove one more.
			missing := make([]byte, shareSize)
			copy(missing, flattened[1])
			flattened[0], flattened[1], flattened[2], flattened[3] = nil, nil, nil, nil
			flattened[4], flattened[5], flattened[6], flattened[7] = nil, nil, nil, nil
			flattened[8], flattened[9], flattened[10] = nil, nil, nil
			flattened[12], flattened[13] = nil, nil

			// Re-import the data square.
			eds, err = rsmt2d.ImportExtendedDataSquare(flattened, tt.codec, rsmt2d.NewDefaultTree)
			if err != nil {
				t.Errorf("ImportExtendedDataSquare failed: %v", err)
			}

			// Repair square.
			err = eds.Repair(
				rowRoots,
				colRoots,
			)
			if !errors.Is(err, rsmt2d.ErrUnrepairableDataSquare) {
				// Should fail since insufficient data.
				t.Errorf("RepairExtendedDataSquare did not fail with `%v`, got `%v`", rsmt2d.ErrUnrepairableDataSquare, err)
			}
			// Re-insert missing share and try again.
			flattened[1] = make([]byte, shareSize)
			copy(flattened[1], missing)

			// Re-import the data square.
			eds, err = rsmt2d.ImportExtendedDataSquare(flattened, tt.codec, rsmt2d.NewDefaultTree)
			if err != nil {
				t.Errorf("ImportExtendedDataSquare failed: %v", err)
			}

			err = eds.Repair(
				rowRoots,
				colRoots,
			)
			if err != nil {
				// Should now pass, since sufficient data.
				t.Errorf("RepairExtendedDataSquare failed: %v", err)
			}
		})
	}
}

// TestRepairWithOneQuarterPopulated is motivated by a use case from
// celestia-node. It verifies that a new EDS can be populated via SetCell. After
// enough shares have been populated, it verifies that the EDS can be repaired.
// After the EDS is repaired, the test verifies that data in a repaired cell
// matches the expected data.
func TestRepairWithOneQuarterPopulated(t *testing.T) {
	edsWidth := 4
	shareSize := 512

	exampleEds := createExampleEds(t, shareSize)

	eds, err := rsmt2d.NewExtendedDataSquare(rsmt2d.NewLeoRSCodec(), rsmt2d.NewDefaultTree, uint(edsWidth), uint(shareSize))
	require.NoError(t, err)

	// Populate EDS with 1/4 of shares using SetCell
	err = eds.SetCell(0, 0, exampleEds.GetCell(0, 0))
	require.NoError(t, err)
	err = eds.SetCell(0, 1, exampleEds.GetCell(0, 1))
	require.NoError(t, err)
	err = eds.SetCell(1, 0, exampleEds.GetCell(1, 0))
	require.NoError(t, err)
	err = eds.SetCell(1, 1, exampleEds.GetCell(1, 1))
	require.NoError(t, err)

	// Verify that an unpopulated cell returns nil
	assert.Nil(t, eds.GetCell(3, 3))

	rowRoots, err := exampleEds.RowRoots()
	require.NoError(t, err)
	colRoots, err := exampleEds.ColRoots()
	require.NoError(t, err)

	// Repair the EDS
	err = eds.Repair(rowRoots, colRoots)
	assert.NoError(t, err)

	assert.Equal(t, exampleEds.Flattened(), eds.Flattened())
}

func createExampleEds(t *testing.T, shareSize int) (eds *rsmt2d.ExtendedDataSquare) {
	ones := bytes.Repeat([]byte{1}, shareSize)
	twos := bytes.Repeat([]byte{2}, shareSize)
	threes := bytes.Repeat([]byte{3}, shareSize)
	fours := bytes.Repeat([]byte{4}, shareSize)
	ods := [][]byte{
		ones, twos,
		threes, fours,
	}

	eds, err := rsmt2d.ComputeExtendedDataSquare(ods, rsmt2d.NewLeoRSCodec(), rsmt2d.NewDefaultTree)
	require.NoError(t, err)
	return eds
}

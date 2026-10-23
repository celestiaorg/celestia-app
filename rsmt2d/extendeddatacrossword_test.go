package rsmt2d

import (
	"bytes"
	crand "crypto/rand"
	"errors"
	"fmt"
	"math/rand"
	"testing"

	"github.com/celestiaorg/nmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// shareSize is the size of each share (in bytes) used for testing.
const shareSize = 512

// PseudoFraudProof is an example fraud proof.
// TODO a real fraud proof would have a Merkle proof for each share.
type PseudoFraudProof struct {
	Mode   int      // Row (0) or column (1)
	Index  uint     // Row or column index
	Shares [][]byte // Bad shares (nil are missing)
}

func TestRepairExtendedDataSquare(t *testing.T) {
	codec := NewLeoRSCodec()
	original := createTestEds(codec, shareSize)

	rowRoots, err := original.RowRoots()
	require.NoError(t, err)

	colRoots, err := original.ColRoots()
	require.NoError(t, err)

	// Verify that an EDS can be repaired after the maximum amount of erasures
	t.Run("MaximumErasures", func(t *testing.T) {
		flattened := original.Flattened()
		flattened[0], flattened[2], flattened[3] = nil, nil, nil
		flattened[4], flattened[5], flattened[6], flattened[7] = nil, nil, nil, nil
		flattened[8], flattened[9], flattened[10] = nil, nil, nil
		flattened[12], flattened[13] = nil, nil

		// Re-import the data square.
		eds, err := ImportExtendedDataSquare(flattened, codec, NewDefaultTree)
		if err != nil {
			t.Errorf("ImportExtendedDataSquare failed: %v", err)
		}

		err = eds.Repair(rowRoots, colRoots)
		if err != nil {
			t.Errorf("unexpected err while repairing data square: %v, codec: :%s", err, codec.Name())
		} else {
			assert.Equal(t, original.GetCell(0, 0), bytes.Repeat([]byte{1}, shareSize))
			assert.Equal(t, original.GetCell(0, 1), bytes.Repeat([]byte{2}, shareSize))
			assert.Equal(t, original.GetCell(1, 0), bytes.Repeat([]byte{3}, shareSize))
			assert.Equal(t, original.GetCell(1, 1), bytes.Repeat([]byte{4}, shareSize))
		}
	})

	// Verify that an EDS returns an error when there are too many erasures
	t.Run("Unrepairable", func(t *testing.T) {
		flattened := original.Flattened()
		flattened[0], flattened[2], flattened[3] = nil, nil, nil
		flattened[4], flattened[5], flattened[6], flattened[7] = nil, nil, nil, nil
		flattened[8], flattened[9], flattened[10] = nil, nil, nil
		flattened[12], flattened[13], flattened[14] = nil, nil, nil

		// Re-import the data square.
		eds, err := ImportExtendedDataSquare(flattened, codec, NewDefaultTree)
		if err != nil {
			t.Errorf("ImportExtendedDataSquare failed: %v", err)
		}

		err = eds.Repair(rowRoots, colRoots)
		if err != ErrUnrepairableDataSquare {
			t.Errorf("did not return an error on trying to repair an unrepairable square")
		}
	})

	t.Run("repair in random order", func(t *testing.T) {
		for i := 0; i < 100; i++ {
			newEds, err := NewExtendedDataSquare(codec, NewDefaultTree, original.Width(), shareSize)
			require.NoError(t, err)
			// Randomly set shares in the newEds from the original and repair.
			for {
				x := rand.Intn(int(original.Width()))
				y := rand.Intn(int(original.Width()))
				if newEds.GetCell(uint(x), uint(y)) != nil {
					continue
				}
				err = newEds.SetCell(uint(x), uint(y), original.GetCell(uint(x), uint(y)))
				require.NoError(t, err)

				// Repair square.
				err = newEds.Repair(rowRoots, colRoots)
				if errors.Is(err, ErrUnrepairableDataSquare) {
					continue
				}
				require.NoError(t, err)
				break
			}

			require.True(t, newEds.Equals(original))
			newRowRoots, err := newEds.RowRoots()
			require.NoError(t, err)
			require.Equal(t, rowRoots, newRowRoots)
			newColRoots, err := newEds.ColRoots()
			require.NoError(t, err)
			require.Equal(t, colRoots, newColRoots)
		}
	})
}

func TestValidFraudProof(t *testing.T) {
	codec := NewLeoRSCodec()

	corruptShare := bytes.Repeat([]byte{66}, shareSize)

	original := createTestEds(codec, shareSize)

	var byzData *ErrByzantineData
	corrupted, err := original.deepCopy(codec)
	if err != nil {
		t.Fatalf("unexpected err while copying original data: %v, codec: :%s", err, codec.Name())
	}
	corrupted.setCell(0, 0, corruptShare)
	assert.NoError(t, err)

	rowRoots, err := corrupted.getRowRoots()
	assert.NoError(t, err)

	colRoots, err := corrupted.getColRoots()
	assert.NoError(t, err)

	err = corrupted.Repair(rowRoots, colRoots)
	errors.As(err, &byzData)

	// Construct the fraud proof
	fraudProof := PseudoFraudProof{0, byzData.Index, byzData.Shares}
	// Verify the fraud proof
	// TODO in a real fraud proof, also verify Merkle proof for each non-nil share.
	rebuiltShares, err := codec.Decode(fraudProof.Shares)
	if err != nil {
		t.Errorf("could not decode fraud proof shares; got: %v", err)
	}
	root, err := corrupted.computeSharesRoot(rebuiltShares, byzData.Axis, fraudProof.Index)
	assert.NoError(t, err)
	rowRoot, err := corrupted.getRowRoot(fraudProof.Index)
	assert.NoError(t, err)
	if bytes.Equal(root, rowRoot) {
		// If the roots match, then the fraud proof should be for invalid erasure coding.
		parityShares, err := codec.Encode(rebuiltShares[0:corrupted.originalDataWidth])
		if err != nil {
			t.Errorf("could not encode fraud proof shares; %v", fraudProof)
		}
		startIndex := len(rebuiltShares) - int(corrupted.originalDataWidth)
		if bytes.Equal(flattenShares(parityShares), flattenShares(rebuiltShares[startIndex:])) {
			t.Errorf("invalid fraud proof %v", fraudProof)
		}
	}
}

func TestCannotRepairSquareWithBadRoots(t *testing.T) {
	codec := NewLeoRSCodec()

	corruptShare := bytes.Repeat([]byte{66}, shareSize)
	original := createTestEds(codec, shareSize)

	rowRoots, err := original.RowRoots()
	require.NoError(t, err)

	colRoots, err := original.ColRoots()
	require.NoError(t, err)

	original.setCell(0, 0, corruptShare)
	require.NoError(t, err)
	err = original.Repair(rowRoots, colRoots)
	if err == nil {
		t.Errorf("did not return an error on trying to repair a square with bad roots")
	}
}

func TestCorruptedEdsReturnsErrByzantineData(t *testing.T) {
	corruptShare := bytes.Repeat([]byte{66}, shareSize)

	tests := []struct {
		name   string
		coords [][]uint
		values [][]byte
	}{
		{
			name:   "corrupt a share in the original data square",
			coords: [][]uint{{0, 0}},
			values: [][]byte{corruptShare},
		},
		{
			name:   "corrupt a share in the extended data square",
			coords: [][]uint{{0, 3}},
			values: [][]byte{corruptShare},
		},
		{
			name:   "corrupt a share at (0, 0) and delete shares from the rest of the row",
			coords: [][]uint{{0, 0}, {0, 1}, {0, 2}, {0, 3}},
			values: [][]byte{corruptShare, nil, nil, nil},
		},
		{
			name:   "corrupt a share at (3, 0) and delete part of the first row ",
			coords: [][]uint{{3, 0}, {0, 1}, {0, 2}, {0, 3}},
			values: [][]byte{corruptShare, nil, nil, nil},
		},
		{
			// This test case sets all shares along the diagonal to nil so that
			// the prerepairSanityCheck does not return an error and it can
			// verify that solveCrossword returns an ErrByzantineData with
			// shares populated.
			name: "set all shares along the diagonal to nil and then corrupt the cell at (0, 1)",
			// In the ASCII diagram below, _ represents a nil share and C
			// represents a corrupted share.
			//
			// _ C O O
			// O _ O O
			// O O _ O
			// O O O _
			coords: [][]uint{{0, 0}, {1, 1}, {2, 2}, {3, 3}, {0, 1}},
			values: [][]byte{nil, nil, nil, nil, corruptShare},
		},
	}

	codec := NewLeoRSCodec()

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			eds := createTestEds(codec, shareSize)

			// compute the rowRoots prior to corruption
			rowRoots, err := eds.getRowRoots()
			assert.NoError(t, err)

			// compute the colRoots prior to corruption
			colRoots, err := eds.getColRoots()
			assert.NoError(t, err)

			for i, coords := range test.coords {
				rowIdx := coords[0]
				colIdx := coords[1]
				eds.setCell(rowIdx, colIdx, test.values[i])
			}

			err = eds.Repair(rowRoots, colRoots)
			assert.Error(t, err)

			// due to parallelisation, the ErrByzantineData axis may be either row or col
			var byzData *ErrByzantineData
			assert.ErrorAs(t, err, &byzData, "did not return a ErrByzantineData for a bad col or row")
			assert.NotEmpty(t, byzData.Shares)
			assert.Contains(t, byzData.Shares, corruptShare)
		})
	}
}

// TestRepairVerifiesOrthogonalVectorsAgainstRoots is a regression test for
// https://github.com/celestiaorg/rsmt2d/issues/118. It guards the fix from
// PR #114 (originally reported as #113): when a row solve newly completes an
// orthogonal column (or a column solve completes a row), that vector must be
// verified against its committed Merkle root including the just-rebuilt share.
// Before the fix this check was skipped because the EDS was only updated after
// the check, so a square that violates its column/row roots passed repair
// silently.
//
// The corruption below is only revealed when an orthogonal vector is completed
// during a row/column solve, so detection depends on the fix: with the
// orthogonal-vector check reverted, Repair returns no error.
func TestRepairVerifiesOrthogonalVectorsAgainstRoots(t *testing.T) {
	corruptShare := bytes.Repeat([]byte{66}, shareSize)
	codec := NewLeoRSCodec()

	eds := createTestEds(codec, shareSize)

	// Capture the honest roots before corrupting the square.
	rowRoots, err := eds.getRowRoots()
	require.NoError(t, err)
	colRoots, err := eds.getColRoots()
	require.NoError(t, err)

	// Delete two shares so no row or column is initially complete (this stops
	// preRepairSanityCheck from short-circuiting) and corrupt (2, 2) so the
	// corruption is only surfaced when an orthogonal vector is completed during
	// a solve.
	//
	// O O _ O      _ = nil share
	// O O O O      C = corrupted share
	// _ O C O      O = original/parity share
	// O O O O
	eds.setCell(0, 2, nil)
	eds.setCell(2, 0, nil)
	eds.setCell(2, 2, corruptShare)

	err = eds.Repair(rowRoots, colRoots)
	require.Error(t, err, "repair must reject a square whose completed orthogonal vector violates its root")

	var byzData *ErrByzantineData
	require.ErrorAs(t, err, &byzData, "repair must detect the corruption as Byzantine data via the orthogonal-vector check")
	// The corruption is caught when row 0's rebuild of (0, 2) completes column
	// 2, so the Byzantine error must name column 2 specifically. Asserting the
	// axis and index guards against the test passing due to an unrelated error.
	require.Equal(t, Col, byzData.Axis, "Byzantine error must be on the column axis")
	require.Equal(t, uint(2), byzData.Index, "Byzantine error must be on column index 2")
}

// TestErrByzantineDataSharesMatchOrthogonalAxis is a regression test for
// GHSA-jfh3-xj5q-rm8x. When Repair detects byzantine data on an orthogonal axis
// (e.g. a column completed during a row solve), ErrByzantineData.Shares must
// carry the shares of the axis named by the error (Axis/Index), not the shares
// of the axis currently being solved. Previously the solver attached the
// in-progress row's shares to a column-axis error, so a downstream consumer
// building a bad-encoding fraud proof would point at the wrong vector and the
// proof would be unverifiable.
func TestErrByzantineDataSharesMatchOrthogonalAxis(t *testing.T) {
	corruptShare := bytes.Repeat([]byte{66}, shareSize)
	codec := NewLeoRSCodec()

	eds := createTestEds(codec, shareSize)
	rowRoots, err := eds.getRowRoots()
	require.NoError(t, err)
	colRoots, err := eds.getColRoots()
	require.NoError(t, err)

	// Same corruption as TestRepairVerifiesOrthogonalVectorsAgainstRoots: the
	// byzantine cell (2, 2) is only surfaced when row 0's rebuild of (0, 2)
	// completes column 2, so the error names column 2 (orthogonal to the row
	// solve in progress).
	//
	// O O _ O      _ = nil share
	// O O O O      C = corrupted share
	// _ O C O      O = original/parity share
	// O O O O
	eds.setCell(0, 2, nil)
	eds.setCell(2, 0, nil)
	eds.setCell(2, 2, corruptShare)

	err = eds.Repair(rowRoots, colRoots)
	var byzData *ErrByzantineData
	require.ErrorAs(t, err, &byzData)
	require.Equal(t, Col, byzData.Axis, "Byzantine error must be on the column axis")
	require.Equal(t, uint(2), byzData.Index, "Byzantine error must be on column index 2")

	// The shares attached to the error must be column 2's shares. Column 2
	// contains the corrupt cell at (2, 2); row 0 (the axis being solved) does
	// not. Before the fix Shares was set to row 0, so this assertion fails.
	require.Equal(t, int(eds.Width()), len(byzData.Shares))
	assert.Contains(t, byzData.Shares, corruptShare,
		"Shares must contain the corrupt cell that lives on the byzantine (column) axis")
	// The share at the rebuilt index (row 0 of column 2) was missing prior to
	// repair and must remain nil, as the ErrByzantineData doc comment promises.
	assert.Nil(t, byzData.Shares[0],
		"missing share at the rebuilt index must remain nil")
}

// TestErrByzantineDataSharesPreserveNils is a regression test for
// GHSA-jfh3-xj5q-rm8x. The ErrByzantineData doc comment promises that missing
// shares are nil, but LeoRSCodec.Decode reconstructs missing shares in place.
// The solver previously attached that mutated buffer to the error, so the nil
// placeholders a fraud-proof consumer relies on were silently filled with
// reconstructed (uncommitted) shares. Repair must instead return a snapshot of
// the byzantine row/column prior to repair, with missing shares as nil.
func TestErrByzantineDataSharesPreserveNils(t *testing.T) {
	corruptShare := bytes.Repeat([]byte{66}, shareSize)
	codec := NewLeoRSCodec()

	eds := createTestEds(codec, shareSize)
	rowRoots, err := eds.getRowRoots()
	require.NoError(t, err)
	colRoots, err := eds.getColRoots()
	require.NoError(t, err)

	// Corrupt (0, 0) and remove (0, 2), (0, 3), and (3, 0). No row or column is
	// complete pre-repair, so preRepairSanityCheck does not short-circuit. When
	// solveCrosswordRow rebuilds row 0 from the corrupt (0, 0) and honest (0, 1),
	// the rebuilt row fails its own row root, producing a Row-axis byzantine
	// error whose Shares must preserve the two nils at (0, 2) and (0, 3).
	//
	// C O _ _      _ = nil share
	// O O O O      C = corrupted share
	// O O O O      O = original/parity share
	// _ O O O
	eds.setCell(0, 0, corruptShare)
	eds.setCell(0, 2, nil)
	eds.setCell(0, 3, nil)
	eds.setCell(3, 0, nil)

	err = eds.Repair(rowRoots, colRoots)
	var byzData *ErrByzantineData
	require.ErrorAs(t, err, &byzData)
	require.Equal(t, Row, byzData.Axis, "Byzantine error must be on the row axis")
	require.Equal(t, uint(0), byzData.Index, "Byzantine error must be on row index 0")

	require.Equal(t, int(eds.Width()), len(byzData.Shares))
	assert.Contains(t, byzData.Shares, corruptShare)
	// Positions 2 and 3 of row 0 were missing prior to repair. Before the fix,
	// Decode filled them in place, so they were non-nil.
	assert.Nil(t, byzData.Shares[2], "missing share at (0, 2) must remain nil")
	assert.Nil(t, byzData.Shares[3], "missing share at (0, 3) must remain nil")
}

func BenchmarkRepair(b *testing.B) {
	// For different ODS sizes
	for originalDataWidth := 4; originalDataWidth <= 512; originalDataWidth *= 2 {
		codec := NewLeoRSCodec()
		if codec.MaxChunks() < originalDataWidth*originalDataWidth {
			// Only test codecs that support this many shares
			continue
		}

		// Generate a new range original data square then extend it
		square := genRandDS(originalDataWidth, shareSize)
		eds, err := ComputeExtendedDataSquare(square, codec, NewDefaultTree)
		if err != nil {
			b.Error(err)
		}

		extendedDataWidth := originalDataWidth * 2
		rowRoots, err := eds.RowRoots()
		assert.NoError(b, err)

		colRoots, err := eds.ColRoots()
		assert.NoError(b, err)

		b.Run(
			fmt.Sprintf(
				"%s %dx%dx%d ODS",
				codec.Name(),
				originalDataWidth,
				originalDataWidth,
				len(square[0]),
			),
			func(b *testing.B) {
				for b.Loop() {
					b.StopTimer()

					flattened := eds.Flattened()
					// Randomly remove 1/2 of the shares of each row
					for r := 0; r < extendedDataWidth; r++ {
						for c := 0; c < originalDataWidth; {
							ind := rand.Intn(extendedDataWidth)
							if flattened[r*extendedDataWidth+ind] == nil {
								continue
							}
							flattened[r*extendedDataWidth+ind] = nil
							c++
						}
					}

					// Re-import the data square.
					eds, _ = ImportExtendedDataSquare(flattened, codec, NewDefaultTree)

					b.StartTimer()

					err := eds.Repair(
						rowRoots,
						colRoots,
					)
					if err != nil {
						b.Error(err)
					}
				}
			},
		)
	}
}

func createTestEds(codec Codec, shareSize int) *ExtendedDataSquare {
	ones := bytes.Repeat([]byte{1}, shareSize)
	twos := bytes.Repeat([]byte{2}, shareSize)
	threes := bytes.Repeat([]byte{3}, shareSize)
	fours := bytes.Repeat([]byte{4}, shareSize)

	eds, err := ComputeExtendedDataSquare([][]byte{
		ones, twos,
		threes, fours,
	}, codec, NewDefaultTree)
	if err != nil {
		panic(err)
	}

	return eds
}

func TestCorruptedEdsReturnsErrByzantineData_UnorderedShares(t *testing.T) {
	shareSize := 512
	namespaceSize := 1
	one := bytes.Repeat([]byte{1}, shareSize)
	two := bytes.Repeat([]byte{2}, shareSize)
	three := bytes.Repeat([]byte{3}, shareSize)
	sharesValue := []int{1, 2, 3, 4}
	tests := []struct {
		name           string
		coords         [][]uint
		values         [][]byte
		wantErr        bool
		corruptedAxis  Axis
		corruptedIndex uint
	}{
		{
			name:    "no corruption",
			wantErr: false,
		},
		{
			// disturbs the order of shares in the first row, erases the rest of the eds
			name:          "rows with unordered shares",
			wantErr:       true, // repair should error out during root construction
			corruptedAxis: Row,
			coords: [][]uint{
				{0, 0},
				{0, 1},
				{1, 0},
				{1, 1},
				{1, 2},
				{1, 3},
				{2, 0},
				{2, 1},
				{2, 2},
				{2, 3},
				{3, 0},
				{3, 1},
				{3, 2},
				{3, 3},
			},
			values: [][]byte{
				two, one,
				nil, nil, nil, nil,
				nil, nil, nil, nil,
				nil, nil, nil, nil,
			},
			corruptedIndex: 0,
		},
		{
			// disturbs the order of shares in the first column, erases the rest of the eds
			name:          "columns with unordered shares",
			wantErr:       true, // repair should error out during root construction
			corruptedAxis: Col,
			coords: [][]uint{
				{0, 0},
				{0, 1},
				{0, 2},
				{0, 3},
				{1, 0},
				{1, 1},
				{1, 2},
				{1, 3},
				{2, 1},
				{2, 2},
				{2, 3},
				{3, 1},
				{3, 2},
				{3, 3},
			},
			values: [][]byte{
				three, nil, nil, nil,
				one, nil, nil, nil,
				nil, nil, nil,
				nil, nil, nil,
			},
			corruptedIndex: 0,
		},
	}

	codec := NewLeoRSCodec()

	// create a DA header
	eds := createTestEdsWithNMT(t, codec, shareSize, namespaceSize, 1, 2, 3, 4)
	assert.NotNil(t, eds)
	dAHeaderRoots, err := eds.getRowRoots()
	assert.NoError(t, err)

	dAHeaderCols, err := eds.getColRoots()
	assert.NoError(t, err)
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			// create an eds with the given shares
			corruptEds := createTestEdsWithNMT(t, codec, shareSize, namespaceSize, sharesValue...)
			assert.NotNil(t, corruptEds)
			// corrupt it by setting the values at the given coordinates
			for i, coords := range test.coords {
				rowIdx := coords[0]
				colIdx := coords[1]
				corruptEds.setCell(rowIdx, colIdx, test.values[i])
			}

			err = corruptEds.Repair(dAHeaderRoots, dAHeaderCols)
			assert.Equal(t, err != nil, test.wantErr)
			if test.wantErr {
				var byzErr *ErrByzantineData
				assert.ErrorAs(t, err, &byzErr)
				errors.As(err, &byzErr)
				assert.Equal(t, byzErr.Axis, test.corruptedAxis)
				assert.Equal(t, byzErr.Index, test.corruptedIndex)
			}
		})
	}
}

func TestFuzzRandByzantine(t *testing.T) {
	// This test is slow and should be skipped during normal testing
	t.Skip()
	for i := 0; i < 10000; i++ {
		TestErrRandByzantine(t)
	}
}

func TestErrRandByzantine(t *testing.T) {
	codec := NewLeoRSCodec()
	original, corrupted, idx := randCorruptedEDS(t, codec, 8)
	require.False(t, original.Equals(corrupted), "corrupted eds is equal to original eds")

	newEds, err := repairNewFromCorrupted(codec, corrupted, idx)
	if err != nil && newEds != nil {
		// visual check of the new eds
		prettyPrintEds(newEds)
		fmt.Println("new eds is original", original.Equals(newEds))
		fmt.Println("new eds is corrupted", corrupted.Equals(newEds))
	}
	require.NoError(t, err, "failure to reconstruct the extended data square")
}

func randCorruptedEDS(t require.TestingT, codec Codec, size int) (original, corrupted *ExtendedDataSquare, idx int) {
	ds := genRandDS(size, shareSize)
	original, err := ComputeExtendedDataSquare(ds, codec, NewDefaultTree)
	require.NoError(t, err)

	// create random share
	randShare := make([]byte, shareSize)
	_, _ = crand.Read(randShare)

	// choose a random share to corrupt
	shares := original.Flattened()
	idx = rand.Intn(len(shares))

	// copy namespace to avoid namespace ordering issues
	copy(randShare, shares[idx][:nmt.DefaultNamespaceIDLen])

	// corrupt the share
	shares[idx] = randShare

	corrupted, err = ImportExtendedDataSquare(
		shares,
		codec,
		NewDefaultTree)
	require.NoError(t, err)
	return original, corrupted, idx
}

func repairNewFromCorrupted(codec Codec, corrupted *ExtendedDataSquare, corruptedIdx int) (*ExtendedDataSquare, error) {
	samples := make([][]bool, corrupted.Width())
	for i := range samples {
		samples[i] = make([]bool, corrupted.Width())
	}

	square, err := NewExtendedDataSquare(
		codec,
		NewDefaultTree,
		corrupted.Width(),
		shareSize,
	)
	if err != nil {
		return nil, fmt.Errorf("failure to create extended data square: %w", err)
	}

	// set corrupted share first
	corruptedX, corruptedY := corruptedIdx/int(corrupted.Width()), corruptedIdx%int(corrupted.Width())
	share := corrupted.GetCell(uint(corruptedX), uint(corruptedY))
	err = square.SetCell(uint(corruptedX), uint(corruptedY), share)
	if err != nil {
		return nil, fmt.Errorf("failure to set corrupted share: %w", err)
	}

	rowRoots, err := corrupted.RowRoots()
	if err != nil {
		return nil, fmt.Errorf("failure to get row roots: %w", err)
	}
	colRoots, err := corrupted.ColRoots()
	if err != nil {
		return nil, fmt.Errorf("failure to get column roots: %w", err)
	}

	// loop until repaired or byzantine error
	for {
		repaired, err := fillRandomCellAndRepair(corrupted, square, rowRoots, colRoots, samples)
		if repaired {
			prettyPrintSamples(samples, corruptedIdx)
			return square, errors.New("no byzantine error")
		}
		var errByz *ErrByzantineData
		if errors.As(err, &errByz) {
			err = checkErrByzantine(errByz, corruptedX, corruptedY)
			if err != nil {
				prettyPrintSamples(samples, corruptedIdx)
			}
			return square, err
		}
	}
}

func fillRandomCellAndRepair(
	eds, square *ExtendedDataSquare,
	rowRoots, colRoots [][]byte,
	samples [][]bool,
) (repaired bool, err error) {
	// select random share
	x, y := rand.Intn(int(eds.Width())), rand.Intn(int(eds.Width()))

	// skip if share is already set
	if square.GetCell(uint(x), uint(y)) != nil {
		return false, nil
	}

	share := eds.GetCell(uint(x), uint(y))
	err = square.SetCell(uint(x), uint(y), share)
	if err != nil {
		return false, fmt.Errorf("failure to set cell: %w", err)
	}
	samples[x][y] = true

	err = square.Repair(rowRoots, colRoots)
	if err != nil {
		return false, err
	}
	return true, nil
}

func checkErrByzantine(errByz *ErrByzantineData, x, y int) error {
	var axisIdx int
	if errByz.Axis == Row {
		axisIdx = x
	} else {
		axisIdx = y
	}

	if errByz.Index != uint(axisIdx) {
		return fmt.Errorf("byzantine error index mismatch: got %s, want %d", errByz, axisIdx)
	}
	return nil
}

// prettyPrintSamples prints coordinates of shares in the 2D array
func prettyPrintSamples(samples [][]bool, corruptedIdx int) {
	fmt.Println("SAMPLES", corruptedIdx)
	for i, row := range samples {
		for j, sampled := range row {
			if corruptedIdx == i*len(samples)+j {
				if !sampled {
					fmt.Print("x ")
					continue
				}
				fmt.Print("X ")
				continue
			}
			if !sampled {
				fmt.Print(". ")
				continue
			}
			fmt.Print("O ")
		}
		fmt.Println()
	}
}

func prettyPrintEds(eds *ExtendedDataSquare) {
	fmt.Println("EDS")
	for r := 0; r < int(eds.Width()); r++ {
		for _, sh := range eds.Row(uint(r)) {
			if sh == nil {
				fmt.Print(". ")
				continue
			}
			fmt.Print("O ")
		}
		fmt.Println()
	}
	fmt.Println()
}

// createTestEdsWithNMT creates an extended data square with the given shares and namespace size.
// Shares are placed in row-major order.
// The first namespaceSize bytes of each share are treated as its namespace.
// Roots of the extended data square are computed using namespace merkle trees.
func createTestEdsWithNMT(t *testing.T, codec Codec, shareSize, namespaceSize int, sharesValue ...int) *ExtendedDataSquare {
	// the first namespaceSize bytes of each share are the namespace
	assert.True(t, shareSize > namespaceSize)

	// create shares of shareSize bytes
	shares := make([][]byte, len(sharesValue))
	for i, shareValue := range sharesValue {
		shares[i] = bytes.Repeat([]byte{byte(shareValue)}, shareSize)
	}
	edsWidth := 4            // number of shares per row/column in the extended data square
	odsWidth := edsWidth / 2 // number of shares per row/column in the original data square

	eds, err := ComputeExtendedDataSquare(shares, codec, newErasuredNamespacedMerkleTreeConstructor(uint64(odsWidth), nmt.NamespaceIDSize(namespaceSize)))
	require.NoError(t, err)

	return eds
}

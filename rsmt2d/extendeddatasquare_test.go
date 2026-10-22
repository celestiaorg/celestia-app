package rsmt2d

import (
	"bytes"
	"crypto/rand"
	"encoding/json"
	"fmt"
	"reflect"
	"sort"
	"testing"

	"github.com/celestiaorg/nmt"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

var (
	zeros     = bytes.Repeat([]byte{0}, shareSize)
	ones      = bytes.Repeat([]byte{1}, shareSize)
	twos      = bytes.Repeat([]byte{2}, shareSize)
	threes    = bytes.Repeat([]byte{3}, shareSize)
	fours     = bytes.Repeat([]byte{4}, shareSize)
	fives     = bytes.Repeat([]byte{5}, shareSize)
	eights    = bytes.Repeat([]byte{8}, shareSize)
	elevens   = bytes.Repeat([]byte{11}, shareSize)
	thirteens = bytes.Repeat([]byte{13}, shareSize)
	fifteens  = bytes.Repeat([]byte{15}, shareSize)
)

func TestComputeExtendedDataSquare(t *testing.T) {
	codec := NewLeoRSCodec()

	type testCase struct {
		name string
		data [][]byte
		want [][][]byte
	}
	testCases := []testCase{
		{
			name: "1x1",
			data: [][]byte{ones},
			want: [][][]byte{
				{ones, ones},
				{ones, ones},
			},
		},
		{
			name: "2x2",
			data: [][]byte{
				ones, twos,
				threes, fours,
			},
			want: [][][]byte{
				{ones, twos, zeros, threes},
				{threes, fours, eights, fifteens},
				{twos, elevens, thirteens, fours},
				{zeros, thirteens, fives, eights},
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			result, err := ComputeExtendedDataSquare(tc.data, codec, NewDefaultTree)
			assert.NoError(t, err)
			assert.Equal(t, tc.want, result.squareRow)
		})
	}

	t.Run("returns an error if shareSize is not a multiple of 64", func(t *testing.T) {
		share := bytes.Repeat([]byte{1}, 65)
		_, err := ComputeExtendedDataSquare([][]byte{share}, NewLeoRSCodec(), NewDefaultTree)
		assert.Error(t, err)
	})
}

func TestImportExtendedDataSquare(t *testing.T) {
	t.Run("is able to import an EDS", func(t *testing.T) {
		eds := createExampleEds(t, shareSize)
		got, err := ImportExtendedDataSquare(eds.Flattened(), NewLeoRSCodec(), NewDefaultTree)
		assert.NoError(t, err)
		assert.Equal(t, eds.Flattened(), got.Flattened())
	})
	t.Run("returns an error if shareSize is not a multiple of 64", func(t *testing.T) {
		share := bytes.Repeat([]byte{1}, 65)
		_, err := ImportExtendedDataSquare([][]byte{share}, NewLeoRSCodec(), NewDefaultTree)
		assert.Error(t, err)
	})
}

func TestMarshalJSON(t *testing.T) {
	codec := NewLeoRSCodec()
	result, err := ComputeExtendedDataSquare([][]byte{
		ones, twos,
		threes, fours,
	}, codec, NewDefaultTree)
	if err != nil {
		panic(err)
	}

	edsBytes, err := json.Marshal(result)
	if err != nil {
		t.Errorf("failed to marshal EDS: %v", err)
	}

	var eds ExtendedDataSquare
	err = json.Unmarshal(edsBytes, &eds)
	if err != nil {
		t.Errorf("failed to marshal EDS: %v", err)
	}
	if !reflect.DeepEqual(result.squareRow, eds.squareRow) {
		t.Errorf("eds not equal after json marshal/unmarshal")
	}
}

func TestNewExtendedDataSquare(t *testing.T) {
	t.Run("returns an error if edsWidth is not even", func(t *testing.T) {
		edsWidth := uint(1)

		_, err := NewExtendedDataSquare(NewLeoRSCodec(), NewDefaultTree, edsWidth, shareSize)
		assert.Error(t, err)
	})
	t.Run("returns an error if shareSize is not a multiple of 64", func(t *testing.T) {
		edsWidth := uint(1)
		shareSize := uint(65)

		_, err := NewExtendedDataSquare(NewLeoRSCodec(), NewDefaultTree, edsWidth, shareSize)
		assert.Error(t, err)
	})
	t.Run("returns a 4x4 EDS", func(t *testing.T) {
		edsWidth := uint(4)

		got, err := NewExtendedDataSquare(NewLeoRSCodec(), NewDefaultTree, edsWidth, shareSize)
		assert.NoError(t, err)
		assert.Equal(t, edsWidth, got.width)
		assert.Equal(t, uint(shareSize), got.shareSize)
	})
	t.Run("returns a 4x4 EDS that can be populated via SetCell", func(t *testing.T) {
		edsWidth := uint(4)

		got, err := NewExtendedDataSquare(NewLeoRSCodec(), NewDefaultTree, edsWidth, shareSize)
		assert.NoError(t, err)

		share := bytes.Repeat([]byte{1}, int(shareSize))
		err = got.SetCell(0, 0, share)
		assert.NoError(t, err)
		assert.Equal(t, share, got.squareRow[0][0])
	})
	t.Run("returns an error when SetCell is invoked on an EDS with a share that is not the correct size", func(t *testing.T) {
		edsWidth := uint(4)
		incorrectShareSize := shareSize + 1

		got, err := NewExtendedDataSquare(NewLeoRSCodec(), NewDefaultTree, edsWidth, shareSize)
		assert.NoError(t, err)

		share := bytes.Repeat([]byte{1}, incorrectShareSize)
		err = got.SetCell(0, 0, share)
		assert.Error(t, err)
	})
}

func TestImmutableRoots(t *testing.T) {
	codec := NewLeoRSCodec()
	result, err := ComputeExtendedDataSquare([][]byte{
		ones, twos,
		threes, fours,
	}, codec, NewDefaultTree)
	if err != nil {
		panic(err)
	}

	mutatedRowRoots, err := result.RowRoots()
	assert.NoError(t, err)

	mutatedRowRoots[0][0]++ // mutate

	rowRoots, err := result.RowRoots()
	assert.NoError(t, err)

	if reflect.DeepEqual(mutatedRowRoots, rowRoots) {
		t.Errorf("Exported EDS RowRoots was mutable")
	}

	mutatedColRoots, err := result.ColRoots()
	assert.NoError(t, err)

	mutatedColRoots[0][0]++ // mutate

	colRoots, err := result.ColRoots()
	assert.NoError(t, err)

	if reflect.DeepEqual(mutatedColRoots, colRoots) {
		t.Errorf("Exported EDS ColRoots was mutable")
	}
}

func TestEDSRowColImmutable(t *testing.T) {
	codec := NewLeoRSCodec()
	result, err := ComputeExtendedDataSquare([][]byte{
		ones, twos,
		threes, fours,
	}, codec, NewDefaultTree)
	if err != nil {
		panic(err)
	}

	row := result.Row(0)
	row[0][0]++
	if reflect.DeepEqual(row, result.Row(0)) {
		t.Errorf("Exported EDS Row was mutable")
	}

	col := result.Col(0)
	col[0][0]++
	if reflect.DeepEqual(col, result.Col(0)) {
		t.Errorf("Exported EDS Col was mutable")
	}
}

func TestRowRoots(t *testing.T) {
	t.Run("returns row roots for a 4x4 EDS", func(t *testing.T) {
		eds, err := ComputeExtendedDataSquare([][]byte{
			ones, twos,
			threes, fours,
		}, NewLeoRSCodec(), NewDefaultTree)
		require.NoError(t, err)

		rowRoots, err := eds.RowRoots()
		assert.NoError(t, err)
		assert.Len(t, rowRoots, 4)
	})

	t.Run("returns an error for an incomplete EDS", func(t *testing.T) {
		eds, err := ComputeExtendedDataSquare([][]byte{
			ones, twos,
			threes, fours,
		}, NewLeoRSCodec(), NewDefaultTree)
		require.NoError(t, err)

		// set a cell to nil to make the EDS incomplete
		eds.setCell(0, 0, nil)

		_, err = eds.RowRoots()
		assert.Error(t, err)
	})
}

func TestColRoots(t *testing.T) {
	t.Run("returns col roots for a 4x4 EDS", func(t *testing.T) {
		eds, err := ComputeExtendedDataSquare([][]byte{
			ones, twos,
			threes, fours,
		}, NewLeoRSCodec(), NewDefaultTree)
		require.NoError(t, err)

		colRoots, err := eds.ColRoots()
		assert.NoError(t, err)
		assert.Len(t, colRoots, 4)
	})

	t.Run("returns an error for an incomplete EDS", func(t *testing.T) {
		eds, err := ComputeExtendedDataSquare([][]byte{
			ones, twos,
			threes, fours,
		}, NewLeoRSCodec(), NewDefaultTree)
		require.NoError(t, err)

		// set a cell to nil to make the EDS incomplete
		eds.setCell(0, 0, nil)

		_, err = eds.ColRoots()
		assert.Error(t, err)
	})
}

// dump acts as a data dump for the benchmarks to stop the compiler from making
// unrealistic optimizations
var dump *ExtendedDataSquare

// BenchmarkExtensionEncoding benchmarks extending datasquares sizes 4-128 using all
// supported codecs (encoding only)
func BenchmarkExtensionEncoding(b *testing.B) {
	for i := 4; i < 513; i *= 2 {
		for codecName, codec := range codecs {
			if codec.MaxChunks() < i*i {
				// Only test codecs that support this many shares
				continue
			}

			square := genRandDS(i, shareSize)
			b.Run(
				fmt.Sprintf("%s %dx%dx%d ODS", codecName, i, i, len(square[0])),
				func(b *testing.B) {
					for b.Loop() {
						eds, err := ComputeExtendedDataSquare(square, codec, NewDefaultTree)
						if err != nil {
							b.Error(err)
						}
						dump = eds
					}
				},
			)
		}
	}
}

// BenchmarkExtensionWithRoots benchmarks extending datasquares sizes 4-128 using all
// supported codecs (both encoding and root computation)
func BenchmarkExtensionWithRoots(b *testing.B) {
	for i := 4; i < 513; i *= 2 {
		for codecName, codec := range codecs {
			if codec.MaxChunks() < i*i {
				// Only test codecs that support this many shares
				continue
			}

			square := genRandDS(i, shareSize)
			b.Run(
				fmt.Sprintf("%s %dx%dx%d ODS", codecName, i, i, len(square[0])),
				func(b *testing.B) {
					for b.Loop() {
						eds, err := ComputeExtendedDataSquare(square, codec, NewDefaultTree)
						if err != nil {
							b.Error(err)
						}
						_, _ = eds.RowRoots()
						_, _ = eds.ColRoots()
						dump = eds
					}
				},
			)
		}
	}
}

// genRandDS make a datasquare of random data, with width describing the number
// of shares on a single side of the ds
func genRandDS(width int, shareSize int) [][]byte {
	var ds [][]byte
	count := width * width
	for i := 0; i < count; i++ {
		share := make([]byte, shareSize)
		_, err := rand.Read(share)
		if err != nil {
			panic(err)
		}
		ds = append(ds, share)
	}
	return ds
}

func genRandSortedDS(width int, shareSize int, namespaceSize int) [][]byte {
	ds := genRandDS(width, shareSize)

	// Sort the shares in the square based on their namespace
	sort.Slice(ds, func(i, j int) bool {
		// Compare only the first  namespaceSize bytes
		return bytes.Compare(ds[i][:namespaceSize], ds[j][:namespaceSize]) < 0
	})

	return ds
}

// TestFlattened_EDS tests that eds.Flattened() returns all the shares in the
// EDS. This function has the `_EDS` suffix to avoid a name collision with the
// TestFlattened.
func TestFlattened_EDS(t *testing.T) {
	example := createExampleEds(t, shareSize)
	want := [][]byte{
		ones, twos, zeros, threes,
		threes, fours, eights, fifteens,
		twos, elevens, thirteens, fours,
		zeros, thirteens, fives, eights,
	}

	got := example.Flattened()
	assert.Equal(t, want, got)
}

func TestFlattenedODS(t *testing.T) {
	example := createExampleEds(t, shareSize)
	want := [][]byte{
		ones, twos,
		threes, fours,
	}

	got := example.FlattenedODS()
	assert.Equal(t, want, got)
}

func TestEquals(t *testing.T) {
	t.Run("returns true for two equal EDS", func(t *testing.T) {
		a := createExampleEds(t, shareSize)
		b := createExampleEds(t, shareSize)
		assert.True(t, a.Equals(b))
	})
	t.Run("returns false for two unequal EDS", func(t *testing.T) {
		a := createExampleEds(t, shareSize)

		type testCase struct {
			name  string
			other *ExtendedDataSquare
		}

		unequalOriginalDataWidth := createExampleEds(t, shareSize)
		unequalOriginalDataWidth.originalDataWidth = 1

		unequalCodecs := createExampleEds(t, shareSize)
		unequalCodecs.codec = newTestCodec()

		unequalShareSize := createExampleEds(t, shareSize*2)

		unequalEds, err := ComputeExtendedDataSquare([][]byte{ones}, NewLeoRSCodec(), NewDefaultTree)
		require.NoError(t, err)

		testCases := []testCase{
			{
				name:  "unequal original data width",
				other: unequalOriginalDataWidth,
			},
			{
				name:  "unequal codecs",
				other: unequalCodecs,
			},
			{
				name:  "unequal shareSize",
				other: unequalShareSize,
			},
			{
				name:  "unequalEds",
				other: unequalEds,
			},
		}
		for _, tc := range testCases {
			t.Run(tc.name, func(t *testing.T) {
				assert.False(t, a.Equals(tc.other))
				assert.False(t, reflect.DeepEqual(a, tc.other))
			})
		}
	})
}

func TestRoots(t *testing.T) {
	t.Run("returns roots for a 4x4 EDS", func(t *testing.T) {
		eds, err := ComputeExtendedDataSquare([][]byte{
			ones, twos,
			threes, fours,
		}, NewLeoRSCodec(), NewDefaultTree)
		require.NoError(t, err)

		roots, err := eds.Roots()
		require.NoError(t, err)
		assert.Len(t, roots, 8)

		rowRoots, err := eds.RowRoots()
		require.NoError(t, err)

		colRoots, err := eds.ColRoots()
		require.NoError(t, err)

		assert.Equal(t, roots[0], rowRoots[0])
		assert.Equal(t, roots[1], rowRoots[1])
		assert.Equal(t, roots[2], rowRoots[2])
		assert.Equal(t, roots[3], rowRoots[3])
		assert.Equal(t, roots[4], colRoots[0])
		assert.Equal(t, roots[5], colRoots[1])
		assert.Equal(t, roots[6], colRoots[2])
		assert.Equal(t, roots[7], colRoots[3])
	})

	t.Run("returns an error for an incomplete EDS", func(t *testing.T) {
		eds, err := ComputeExtendedDataSquare([][]byte{
			ones, twos,
			threes, fours,
		}, NewLeoRSCodec(), NewDefaultTree)
		require.NoError(t, err)

		// set a cell to nil to make the EDS incomplete
		eds.setCell(0, 0, nil)

		_, err = eds.Roots()
		assert.Error(t, err)
	})
}

func TestDeepCopy(t *testing.T) {
	original := make([][]byte, 16)
	// fill first 8 shares with random data, leave the rest nil
	for i := range original[:8] {
		original[i] = make([]byte, 4)
		_, err := rand.Read(original[i])
		require.NoError(t, err)
	}

	copied := deepCopy(original)
	require.Equal(t, original, copied)

	// modify the original and ensure the copy is not affected
	original[0][0]++
	require.NotEqual(t, original, copied)
}

func TestComputeExtendedDataSquareVsWithBuffer(t *testing.T) {
	var (
		codec                 = NewLeoRSCodec()
		namespaceIDSizeOption = nmt.NamespaceIDSize(defaultNamespaceIDSize)
	)

	t.Run("error cases", func(t *testing.T) {
		pool := newTreePool(2, 4, namespaceIDSizeOption, nmt.IgnoreMaxNamespace(true))

		t.Run("returns an error if shareSize is not a multiple of 64", func(t *testing.T) {
			share := bytes.Repeat([]byte{1}, 65)
			_, err := ComputeExtendedDataSquareWithBuffer([][]byte{share}, codec, pool)
			require.Error(t, err)
		})
		t.Run("returns an error if number of shares is not a perfect square", func(t *testing.T) {
			shares := make([][]byte, 3)
			for i := range shares {
				shares[i] = bytes.Repeat([]byte{byte(i + 1)}, shareSize)
			}
			_, err := ComputeExtendedDataSquareWithBuffer(shares, codec, pool)
			require.Error(t, err)
		})
	})

	sizes := []struct {
		name    string
		odsSize int
	}{
		{"ods-32", 32},
		{"ods-64", 64},
		{"ods-83", 83},
		{"ods-127", 127},
		{"ods-128", 128},
		{"ods-256", 256},
		{"ods-512", 512},
		// uneven sizes
		{"ods-35", 35},
		{"ods-67", 67},
	}

	t.Run("same-size-pool", func(t *testing.T) {
		for _, tc := range sizes {
			t.Run(tc.name, func(t *testing.T) {
				data := genRandSortedDS(tc.odsSize, shareSize, 8)

				pool := newTreePool(uint(tc.odsSize), 4, namespaceIDSizeOption, nmt.IgnoreMaxNamespace(true))
				constructor := newErasuredNamespacedMerkleTreeConstructor(uint64(tc.odsSize), namespaceIDSizeOption, nmt.IgnoreMaxNamespace(true))

				edsStandard, err := ComputeExtendedDataSquare(data, codec, constructor)
				require.NoError(t, err)

				edsWithBuffer, err := ComputeExtendedDataSquareWithBuffer(data, codec, pool)
				require.NoError(t, err)

				rowRootsStandard, err := edsStandard.RowRoots()
				require.NoError(t, err)
				rowRootsWithBuffer, err := edsWithBuffer.RowRoots()
				require.NoError(t, err)

				colRootsStandard, err := edsStandard.ColRoots()
				require.NoError(t, err)
				colRootsWithBuffer, err := edsWithBuffer.ColRoots()
				require.NoError(t, err)

				require.Equal(t, rowRootsStandard, rowRootsWithBuffer)
				require.Equal(t, colRootsStandard, colRootsWithBuffer)
			})
		}
	})

	t.Run("pool-reallocation", func(t *testing.T) {
		// create a pool initialized with the smallest size
		pool := newTreePool(32, 4, namespaceIDSizeOption, nmt.IgnoreMaxNamespace(true))

		for _, tc := range sizes {
			t.Run(tc.name, func(t *testing.T) {
				data := genRandSortedDS(tc.odsSize, shareSize, 8)

				// use the same pool but with different square sizes to test reallocation
				edsWithBuffer, err := ComputeExtendedDataSquareWithBuffer(data, codec, pool)
				require.NoError(t, err)

				constructor := newErasuredNamespacedMerkleTreeConstructor(uint64(tc.odsSize), namespaceIDSizeOption, nmt.IgnoreMaxNamespace(true))
				edsStandard, err := ComputeExtendedDataSquare(data, codec, constructor)
				require.NoError(t, err)

				rowRootsWithBuffer, err := edsWithBuffer.RowRoots()
				require.NoError(t, err)
				rowRootsStandard, err := edsStandard.RowRoots()
				require.NoError(t, err)

				colRootsWithBuffer, err := edsWithBuffer.ColRoots()
				require.NoError(t, err)
				colRootsStandard, err := edsStandard.ColRoots()
				require.NoError(t, err)

				require.Equal(t, rowRootsStandard, rowRootsWithBuffer, "row roots mismatch for ODS size %d with pool reallocation", tc.odsSize)
				require.Equal(t, colRootsStandard, colRootsWithBuffer, "column roots mismatch for ODS size %d with pool reallocation", tc.odsSize)
			})
		}
	})
}

func createExampleEds(t *testing.T, shareSize int) (eds *ExtendedDataSquare) {
	ones := bytes.Repeat([]byte{1}, shareSize)
	twos := bytes.Repeat([]byte{2}, shareSize)
	threes := bytes.Repeat([]byte{3}, shareSize)
	fours := bytes.Repeat([]byte{4}, shareSize)
	ods := [][]byte{
		ones, twos,
		threes, fours,
	}

	eds, err := ComputeExtendedDataSquare(ods, NewLeoRSCodec(), NewDefaultTree)
	require.NoError(t, err)
	return eds
}

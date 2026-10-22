package rsmt2d

import (
	"bytes"
	crand "crypto/rand"
	"testing"

	"github.com/stretchr/testify/require"
)

// randShare returns a random share of shareSize bytes.
func randShare(t *testing.T) []byte {
	t.Helper()
	b := make([]byte, shareSize)
	_, err := crand.Read(b)
	require.NoError(t, err)
	return b
}

// parityShares returns the k parity shares for the given k data shares.
func parityShares(t *testing.T, codec Codec, data [][]byte) [][]byte {
	t.Helper()
	ext, err := codec.Encode(data)
	require.NoError(t, err)
	switch len(ext) {
	case len(data): // Encode returned parity-only
		return ext
	case 2 * len(data): // Encode returned the full codeword
		return ext[len(data):]
	default:
		t.Fatalf("unexpected Encode output len=%d for %d inputs", len(ext), len(data))
		return nil
	}
}

// isCodeword reports whether a 2k-wide row/column is a valid RS codeword.
func isCodeword(t *testing.T, codec Codec, vec [][]byte) bool {
	t.Helper()
	k := len(vec) / 2
	par := parityShares(t, codec, vec[:k])
	for i := range k {
		if !bytes.Equal(par[i], vec[k+i]) {
			return false
		}
	}
	return true
}

// TestRepairRejectsBadlyEncodedSelfDecode is a regression test for the
// bad-encoding fraud-proof suppression fixed in this change. On the self-decode
// reconstruction path, solveCrosswordRow/solveCrosswordCol verified a rebuilt
// vector only against its committed Merkle root, never re-encoding-checking it.
// Because the underlying codec's Decode only fills missing shares (it never
// validates present ones), a malicious proposer could commit a
// "self-decode-consistent" badly-encoded square — every column a valid codeword
// but a row a non-codeword — that reconstructs "consistently" from a partial
// set of shares. Repair then returned nil instead of *ErrByzantineData, so no
// bad-encoding fraud proof was produced.
//
// The square (ODS k=2, EDS width 4):
//
//	row 0: c0, c1, c2, c3   where c3 = Decode([c0,c1,c2,nil])[3]  -> NOT a codeword
//	row 1: r0, r1, parity   -> a valid codeword
//	rows 2,3: column-extension of rows 0,1 -> every COLUMN is a codeword
func TestRepairRejectsBadlyEncodedSelfDecode(t *testing.T) {
	codec := NewLeoRSCodec()
	const width = 4 // EDS width (ODS k=2)

	// row 0: c0,c1,c2 random; c3 chosen so the row self-decodes from {c0,c1,c2}
	// yet (almost surely) is not a valid RS codeword.
	c0, c1, c2 := randShare(t), randShare(t), randShare(t)
	dec, err := codec.Decode([][]byte{c0, c1, c2, nil})
	require.NoError(t, err)
	row0 := [][]byte{c0, c1, c2, dec[3]}

	// row 1: a genuine codeword.
	r0, r1 := randShare(t), randShare(t)
	p1 := parityShares(t, codec, [][]byte{r0, r1})
	row1 := [][]byte{r0, r1, p1[0], p1[1]}

	// rows 2,3: column-extension of the top two rows so every column is a codeword.
	cells := make([][]byte, width*width)
	for c := range width {
		cells[0*width+c] = row0[c]
		cells[1*width+c] = row1[c]
		colPar := parityShares(t, codec, [][]byte{row0[c], row1[c]})
		cells[2*width+c] = colPar[0]
		cells[3*width+c] = colPar[1]
	}

	// Import the crafted square and commit to ITS roots (the malicious DAH).
	bad, err := ImportExtendedDataSquare(cells, codec, NewDefaultTree)
	require.NoError(t, err)
	rowRoots, err := bad.RowRoots()
	require.NoError(t, err)
	colRoots, err := bad.ColRoots()
	require.NoError(t, err)

	// Sanity: the square is genuinely badly-encoded (row 0 is not a codeword)
	// while every column is a valid codeword.
	require.False(t, isCodeword(t, codec, bad.Row(0)), "row 0 must not be a codeword")
	for c := range uint(width) {
		require.True(t, isCodeword(t, codec, bad.Col(c)), "col %d must be a codeword", c)
	}

	// repairWithMask reconstructs from the subset of cells selected by mask,
	// exactly as a retriever does: NewExtendedDataSquare + SetCell + Repair.
	repairWithMask := func(mask uint) error {
		nd, err := NewExtendedDataSquare(codec, NewDefaultTree, width, shareSize)
		require.NoError(t, err)
		for i := range width * width {
			if mask&(1<<uint(i)) != 0 {
				require.NoError(t, nd.SetCell(uint(i/width), uint(i%width), bad.GetCell(uint(i/width), uint(i%width))))
			}
		}
		return nd.Repair(rowRoots, colRoots)
	}

	// mask 0x437 = cells (0,0),(0,1),(0,2),(1,0),(1,1),(2,2): row 0 self-decodes
	// from {(0,0),(0,1),(0,2)} without ever completing a column first.
	err = repairWithMask(0x437)
	require.Error(t, err, "Repair must reject a badly-encoded square")
	var byzData *ErrByzantineData
	require.ErrorAs(t, err, &byzData, "Repair must return *ErrByzantineData so a fraud proof can be built")

	// Every partial availability that reconstructs the full square must now be
	// rejected: a valid mask can only rebuild the committed (badly-encoded)
	// square, which is never a valid encoding.
	for m := range uint(1 << (width * width)) {
		if err := repairWithMask(m); err == nil {
			t.Fatalf("Repair accepted badly-encoded square for mask %#x", m)
		}
	}
}

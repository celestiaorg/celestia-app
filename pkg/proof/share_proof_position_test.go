package proof

import (
	"bytes"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/celestiaorg/nmt"
	"github.com/cometbft/cometbft/crypto/merkle"
	"github.com/stretchr/testify/require"
)

// testSquare is a 2x2 data square extended to 4x4 with stand-in parity shares.
type testSquare struct {
	shares   [][][]byte // shares[row][col] of the extended square
	rowTrees []*nmt.NamespacedMerkleTree
	roots    [][]byte // row roots, then column roots
	root     []byte
	proofs   []*merkle.Proof
}

const testSquareWidth = 4 // extended square width

var testNamespace = append([]byte{0}, bytes.Repeat([]byte{1}, share.NamespaceIDSize)...)

func newTestSquare() testSquare {
	var sq testSquare
	for r := range testSquareWidth {
		row := make([][]byte, 0, testSquareWidth)
		for c := range testSquareWidth {
			row = append(row, append(append([]byte{}, testNamespace...), bytes.Repeat([]byte{byte(r*testSquareWidth + c)}, share.ShareSize-share.NamespaceSize)...))
		}
		sq.shares = append(sq.shares, row)
	}
	for r := range testSquareWidth {
		tree := sq.tree(func(i int) (int, int) { return r, i })
		sq.rowTrees = append(sq.rowTrees, tree)
		sq.roots = append(sq.roots, must(tree.Root()))
	}
	for c := range testSquareWidth {
		sq.roots = append(sq.roots, must(sq.tree(func(i int) (int, int) { return i, c }).Root()))
	}
	sq.root, sq.proofs = merkle.ProofsFromByteSlices(sq.roots)
	return sq
}

// tree builds the NMT of one row or column, hashing parity shares under the
// parity namespace like the extended data square does.
func (sq testSquare) tree(cell func(int) (int, int)) *nmt.NamespacedMerkleTree {
	tree := nmt.New(appconsts.NewBaseHashFunc(), nmt.NamespaceIDSize(share.NamespaceSize), nmt.IgnoreMaxNamespace(true))
	for i := range testSquareWidth {
		r, c := cell(i)
		ns := testNamespace
		if r >= testSquareWidth/2 || c >= testSquareWidth/2 {
			ns = share.ParitySharesNamespace.Bytes()
		}
		if err := tree.Push(append(append([]byte{}, ns...), sq.shares[r][c]...)); err != nil {
			panic(err)
		}
	}
	return tree
}

func must[T any](v T, err error) T {
	if err != nil {
		panic(err)
	}
	return v
}

type rowRange struct{ row, start, end int }

// shareProof proves the given ranges, one per consecutive row, under ns.
func (sq testSquare) shareProof(ns []byte, ranges ...rowRange) ShareProof {
	sp := ShareProof{NamespaceId: ns[1:], NamespaceVersion: uint32(ns[0]), RowProof: &RowProof{}}
	sp.RowProof.StartRow = uint32(ranges[0].row)
	sp.RowProof.EndRow = uint32(ranges[len(ranges)-1].row)
	for _, r := range ranges {
		p := must(sq.rowTrees[r.row].ProveRange(r.start, r.end))
		sp.Data = append(sp.Data, sq.shares[r.row][r.start:r.end]...)
		sp.ShareProofs = append(sp.ShareProofs, &NMTProof{Start: int32(r.start), End: int32(r.end), Nodes: p.Nodes()})
		mp := sq.proofs[r.row]
		sp.RowProof.RowRoots = append(sp.RowProof.RowRoots, sq.roots[r.row])
		sp.RowProof.Proofs = append(sp.RowProof.Proofs, &Proof{Total: mp.Total, Index: mp.Index, LeafHash: mp.LeafHash, Aunts: mp.Aunts})
	}
	return sp
}

func TestShareProofValidatePositions(t *testing.T) {
	sq := newTestSquare()
	parity := share.ParitySharesNamespace.Bytes()

	testCases := []struct {
		name    string
		sp      func() ShareProof
		wantErr bool
	}{
		{
			name:    "valid proof in one row",
			sp:      func() ShareProof { return sq.shareProof(testNamespace, rowRange{0, 0, 2}) },
			wantErr: false,
		},
		{
			name:    "valid proof across rows",
			sp:      func() ShareProof { return sq.shareProof(testNamespace, rowRange{0, 1, 2}, rowRange{1, 0, 1}) },
			wantErr: false,
		},
		{
			name: "total that is not a power of two",
			sp: func() ShareProof {
				sp := sq.shareProof(testNamespace, rowRange{0, 0, 2})
				sp.RowProof.Proofs[0].Total = 7
				return sp
			},
			wantErr: true,
		},
		{
			name: "row index that does not match the claimed row",
			sp: func() ShareProof {
				sp := sq.shareProof(testNamespace, rowRange{0, 0, 2})
				sp.RowProof.StartRow, sp.RowProof.EndRow = 1, 1
				return sp
			},
			wantErr: true,
		},
		{
			name: "column root presented as a row root",
			sp: func() ShareProof {
				sp := sq.shareProof(testNamespace, rowRange{0, 0, 2})
				col := testSquareWidth // first column root
				mp := sq.proofs[col]
				sp.RowProof.StartRow, sp.RowProof.EndRow = uint32(col), uint32(col)
				sp.RowProof.RowRoots[0] = sq.roots[col]
				sp.RowProof.Proofs[0] = &Proof{Total: mp.Total, Index: mp.Index, LeafHash: mp.LeafHash, Aunts: mp.Aunts}
				return sp
			},
			wantErr: true,
		},
		{
			name:    "ranges that are not contiguous across rows",
			sp:      func() ShareProof { return sq.shareProof(testNamespace, rowRange{0, 1, 2}, rowRange{1, 1, 2}) },
			wantErr: true,
		},
		{
			name:    "range that stops before the end of an earlier row",
			sp:      func() ShareProof { return sq.shareProof(testNamespace, rowRange{0, 0, 1}, rowRange{1, 0, 1}) },
			wantErr: true,
		},
		{
			name:    "parity row",
			sp:      func() ShareProof { return sq.shareProof(parity, rowRange{2, 0, 1}) },
			wantErr: true,
		},
		{
			name:    "parity column",
			sp:      func() ShareProof { return sq.shareProof(parity, rowRange{0, 2, 3}) },
			wantErr: true,
		},
		{
			name: "namespace ID that is not 28 bytes",
			sp: func() ShareProof {
				sp := sq.shareProof(testNamespace, rowRange{0, 0, 2})
				sp.NamespaceId = append(sp.NamespaceId, 1)
				return sp
			},
			wantErr: true,
		},
		{
			name: "nil row proof",
			sp: func() ShareProof {
				sp := sq.shareProof(testNamespace, rowRange{0, 0, 2})
				sp.RowProof = nil
				return sp
			},
			wantErr: true,
		},
		{
			name: "nil share proof",
			sp: func() ShareProof {
				sp := sq.shareProof(testNamespace, rowRange{0, 0, 2})
				sp.ShareProofs[0] = nil
				return sp
			},
			wantErr: true,
		},
		{
			name: "nil row proof entry",
			sp: func() ShareProof {
				sp := sq.shareProof(testNamespace, rowRange{0, 0, 2})
				sp.RowProof.Proofs[0] = nil
				return sp
			},
			wantErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			got := tc.sp().Validate(sq.root)
			if tc.wantErr {
				require.Error(t, got)
				return
			}
			require.NoError(t, got)
		})
	}
}

func TestShareProofVerifyProofMalformed(t *testing.T) {
	sq := newTestSquare()

	sp := sq.shareProof(testNamespace, rowRange{0, 0, 2})
	sp.RowProof.RowRoots = nil
	require.False(t, sp.VerifyProof())

	sp = sq.shareProof(testNamespace, rowRange{0, 0, 2})
	sp.ShareProofs[0] = nil
	require.False(t, sp.VerifyProof())

	sp = sq.shareProof(testNamespace, rowRange{0, 0, 2})
	sp.Data = sp.Data[:1]
	require.False(t, sp.VerifyProof())

	sp = sq.shareProof(testNamespace, rowRange{0, 0, 2})
	sp.RowProof = nil
	require.False(t, sp.VerifyProof())
}

func TestRowProofVerifyProofMalformed(t *testing.T) {
	sq := newTestSquare()

	rp := *sq.shareProof(testNamespace, rowRange{0, 0, 2}).RowProof
	rp.Proofs[0] = nil
	require.False(t, rp.VerifyProof(sq.root))

	rp = *sq.shareProof(testNamespace, rowRange{0, 0, 2}).RowProof
	rp.RowRoots = nil
	require.False(t, rp.VerifyProof(sq.root))
}

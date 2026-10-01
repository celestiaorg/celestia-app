package fibre

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"math/bits"
	"runtime"

	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/field"
	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/merkle"
	"github.com/celestiaorg/celestia-app/v10/pkg/rsema1d/rlc"
)

// RowRootSiblings returns the row tree siblings that link the root of the
// original rows to the row root, lowest first.
func (d *Blob) RowRootSiblings() ([][]byte, error) {
	depth := bits.Len(uint(d.cfg.OriginalRows)) - 1
	var siblings [][]byte
	err := d.RowProofs([]int{0}, func(_ int, _ []byte, proof [][]byte) {
		for _, s := range proof[depth:] {
			siblings = append(siblings, bytes.Clone(s))
		}
	})
	return siblings, err
}

// VerifyCommitment checks that id commits to data. siblings are the
// [Blob.RowRootSiblings] of the encoded blob. It recomputes the original rows
// and their RLC, so it does not trust anything else about the encoding.
func VerifyCommitment(data []byte, id BlobID, siblings [][]byte) error {
	if err := id.Validate(); err != nil {
		return err
	}
	cfg, err := BlobConfigForVersion(id.Version())
	if err != nil {
		return err
	}
	if len(data) == 0 || len(data) > cfg.MaxDataSize {
		return fmt.Errorf("data size %d out of range [1, %d]", len(data), cfg.MaxDataSize)
	}
	k, n := cfg.OriginalRows, cfg.ParityRows
	if want := bits.Len(uint(k+n)) - bits.Len(uint(k)); len(siblings) != want {
		return fmt.Errorf("expected %d row root siblings, got %d", want, len(siblings))
	}

	workers := runtime.GOMAXPROCS(0)
	rowSize := cfg.RowSize(len(data))
	rows := originalRows(data, rowSize, k)

	proof, err := merkle.NewTree(rows, workers).Proof(0)
	if err != nil {
		return err
	}
	rowRoot, err := merkle.RootFromProof(rows[0], 0, append(proof, siblings...))
	if err != nil {
		return err
	}

	rlcs := rlc.Compute(rows, rlc.DeriveCoefficients(rowRoot, k, n, rowSize, workers), workers)
	rlcRoot := merkle.RootFromFunc(make([]byte, k*merkle.NodeSize), func(i int, dst []byte) []byte {
		if cap(dst) == 0 {
			dst = make([]byte, field.GF128Size)
		}
		field.EncodeGF128(dst, rlcs[i])
		return dst
	})

	h := sha256.New()
	h.Write(rowRoot[:])
	h.Write(rlcRoot[:])
	want := id.Commitment()
	if !bytes.Equal(h.Sum(nil), want[:]) {
		return errors.New("commitment does not match data")
	}
	return nil
}

// originalRows lays out the blob header and data over k rows of rowSize bytes,
// zero padded. Full rows alias data.
func originalRows(data []byte, rowSize, k int) [][]byte {
	rows := make([][]byte, k)
	first := make([]byte, rowSize)
	newBlobHeaderV0(len(data)).marshalTo(first)
	off := copy(first[blobHeaderLen:], data)
	rows[0] = first

	zero := make([]byte, rowSize)
	for i := 1; i < k; i++ {
		switch {
		case off+rowSize <= len(data):
			rows[i] = data[off : off+rowSize]
		case off < len(data):
			rows[i] = make([]byte, rowSize)
			copy(rows[i], data[off:])
		default:
			rows[i] = zero
		}
		off = min(off+rowSize, len(data))
	}
	return rows
}

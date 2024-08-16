package da

import (
	"bytes"
	"errors"
	"fmt"
	"math"

	"github.com/celestiaorg/go-square/v2"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/celestiaorg/rsmt2d"
	"github.com/tendermint/tendermint/crypto/merkle"
	"github.com/tendermint/tendermint/types"
	"golang.org/x/exp/constraints"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	daproto "github.com/celestiaorg/celestia-app/v3/pkg/da/proto"
	"github.com/celestiaorg/celestia-app/v3/pkg/wrapper"
)

var (
	maxExtendedSquareWidth = appconsts.DefaultSquareSizeUpperBound * 2
	minExtendedSquareWidth = appconsts.MinSquareSize * 2
)

// DataAvailabilityHeader (DAHeader) contains the row and column roots of the
// erasure coded version of the data in Block.Data. The original Block.Data is
// split into shares and arranged in a square of width squareSize. Then, this
// square is "extended" into an extended data square (EDS) of width 2*squareSize
// by applying Reed-Solomon encoding. For details see Section 5.2 of
// https://arxiv.org/abs/1809.09044 or the Celestia specification:
// https://github.com/celestiaorg/celestia-specs/blob/master/src/specs/data_structures.md#availabledataheader
type DataAvailabilityHeader struct {
	// RowRoot_j = root((M_{j,1} || M_{j,2} || ... || M_{j,2k} ))
	RowRoots [][]byte `json:"row_roots"`
	// ColumnRoot_j = root((M_{1,j} || M_{2,j} || ... || M_{2k,j} ))
	ColumnRoots [][]byte `json:"column_roots"`
	// hash is the Merkle root of the row and column roots. This field is the
	// memoized result from `Hash()`.
	hash []byte
}

// NewDataAvailabilityHeader generates a DataAvailability header using the
// provided extended data square.
func NewDataAvailabilityHeader(eds *rsmt2d.ExtendedDataSquare) (DataAvailabilityHeader, error) {
	rowRoots, err := eds.RowRoots()
	if err != nil {
		return DataAvailabilityHeader{}, err
	}
	colRoots, err := eds.ColRoots()
	if err != nil {
		return DataAvailabilityHeader{}, err
	}

	dah := DataAvailabilityHeader{
		RowRoots:    rowRoots,
		ColumnRoots: colRoots,
	}

	// Generate the hash of the data using the new roots
	dah.Hash()

	return dah, nil
}

func ExtendShares(s [][]byte) (*rsmt2d.ExtendedDataSquare, error) {
	// Check that the length of the square is a power of 2.
	if !square.IsPowerOfTwo(len(s)) {
		return nil, fmt.Errorf("number of shares is not a power of 2: got %d", len(s))
	}
	squareSize := SquareSize(len(s))

	// here we construct a tree
	// Note: uses the nmt wrapper to construct the tree.
	return rsmt2d.ComputeExtendedDataSquare(s, appconsts.DefaultCodec(), wrapper.NewConstructor(uint64(squareSize)))
}

// String returns hex representation of merkle hash of the DAHeader.
func (dah *DataAvailabilityHeader) String() string {
	if dah == nil {
		return "<nil DAHeader>"
	}
	return fmt.Sprintf("%X", dah.Hash())
}

// Equals checks equality of two DAHeaders.
func (dah *DataAvailabilityHeader) Equals(to *DataAvailabilityHeader) bool {
	return bytes.Equal(dah.Hash(), to.Hash())
}

// Hash computes the Merkle root of the row and column roots. Hash memoizes the
// result in `DataAvailabilityHeader.hash`.
func (dah *DataAvailabilityHeader) Hash() []byte {
	if dah == nil {
		return merkle.HashFromByteSlices(nil)
	}
	if len(dah.hash) != 0 {
		return dah.hash
	}

	rowsCount := len(dah.RowRoots)
	slices := make([][]byte, rowsCount+rowsCount)
	copy(slices[0:rowsCount], dah.RowRoots)
	copy(slices[rowsCount:], dah.ColumnRoots)
	// The single data root is computed using a simple binary merkle tree.
	// Effectively being root(rowRoots || columnRoots):
	dah.hash = merkle.HashFromByteSlices(slices)
	return dah.hash
}

func (dah *DataAvailabilityHeader) ToProto() (*daproto.DataAvailabilityHeader, error) {
	if dah == nil {
		return nil, errors.New("nil DataAvailabilityHeader")
	}

	dahp := new(daproto.DataAvailabilityHeader)
	dahp.RowRoots = dah.RowRoots
	dahp.ColumnRoots = dah.ColumnRoots
	return dahp, nil
}

func DataAvailabilityHeaderFromProto(dahp *daproto.DataAvailabilityHeader) (dah *DataAvailabilityHeader, err error) {
	if dahp == nil {
		return nil, errors.New("nil DataAvailabilityHeader")
	}

	dah = new(DataAvailabilityHeader)
	dah.RowRoots = dahp.RowRoots
	dah.ColumnRoots = dahp.ColumnRoots

	return dah, dah.ValidateBasic()
}

// ValidateBasic runs stateless checks on the DataAvailabilityHeader.
func (dah *DataAvailabilityHeader) ValidateBasic() error {
	if dah == nil {
		return errors.New("nil data availability header is not valid")
	}
	if len(dah.ColumnRoots) < minExtendedSquareWidth || len(dah.RowRoots) < minExtendedSquareWidth {
		return fmt.Errorf(
			"minimum valid DataAvailabilityHeader has at least %d row and column roots",
			minExtendedSquareWidth,
		)
	}
	if len(dah.ColumnRoots) > maxExtendedSquareWidth || len(dah.RowRoots) > maxExtendedSquareWidth {
		return fmt.Errorf(
			"maximum valid DataAvailabilityHeader has at most %d row and column roots",
			maxExtendedSquareWidth,
		)
	}
	if len(dah.ColumnRoots) != len(dah.RowRoots) {
		return fmt.Errorf(
			"unequal number of row and column roots: row %d col %d",
			len(dah.RowRoots),
			len(dah.ColumnRoots),
		)
	}
	if err := types.ValidateHash(dah.Hash()); err != nil {
		return fmt.Errorf("wrong hash: %v", err)
	}

	return nil
}

func (dah *DataAvailabilityHeader) IsZero() bool {
	if dah == nil {
		return true
	}
	return len(dah.ColumnRoots) == 0 || len(dah.RowRoots) == 0
}

// SquareSize returns the number of rows in the original data square.
func (dah *DataAvailabilityHeader) SquareSize() int {
	return len(dah.RowRoots) / 2
}

// MinDataAvailabilityHeader returns the minimum valid data availability header.
// It is equal to the data availability header for a block with one tail padding
// share.
func MinDataAvailabilityHeader() DataAvailabilityHeader {
	s := MinShares()
	eds, err := ExtendShares(s)
	if err != nil {
		panic(err)
	}
	dah, err := NewDataAvailabilityHeader(eds)
	if err != nil {
		panic(err)
	}
	return dah
}

// MinShares returns one tail-padded share.
func MinShares() [][]byte {
	return share.ToBytes(EmptySquareShares())
}

// EmptySquare is a copy of the function defined in the square package to avoid
// a circular dependency. TODO deduplicate
func EmptySquareShares() []share.Share {
	return share.TailPaddingShares(appconsts.MinShareCount)
}

// SquareSize is a copy of the function defined in the square package to avoid
// a circular dependency. TODO deduplicate
func SquareSize(len int) int {
	return RoundUpPowerOfTwo(int(math.Ceil(math.Sqrt(float64(len)))))
}

// RoundUpPowerOfTwo returns the next power of two greater than or equal to input.
func RoundUpPowerOfTwo[I constraints.Integer](input I) I {
	var result I = 1
	for result < input {
		result <<= 1
	}
	return result
}

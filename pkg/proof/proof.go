package proof

import (
	"bytes"
	"errors"
	"fmt"
	"math"

	"github.com/celestiaorg/rsmt2d"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/pkg/da"
	"github.com/celestiaorg/celestia-app/v3/pkg/wrapper"
	"github.com/celestiaorg/go-square/v2"
	"github.com/celestiaorg/go-square/v2/share"
	blobtx "github.com/celestiaorg/go-square/v2/tx"
	"github.com/tendermint/tendermint/crypto/merkle"
)

// NewTxInclusionProof returns a new share inclusion proof for the given
// transaction index.
func NewTxInclusionProof(txs [][]byte, txIndex, appVersion uint64) (ShareProof, error) {
	if txIndex >= uint64(len(txs)) {
		return ShareProof{}, fmt.Errorf("txIndex %d out of bounds", txIndex)
	}

	builder, err := square.NewBuilder(appconsts.SquareSizeUpperBound(appVersion), appconsts.SubtreeRootThreshold(appVersion), txs...)
	if err != nil {
		return ShareProof{}, err
	}

	dataSquare, err := builder.Export()
	if err != nil {
		return ShareProof{}, err
	}

	txIndexInt, err := safeConvertUint64ToInt(txIndex)
	if err != nil {
		return ShareProof{}, err
	}
	shareRange, err := builder.FindTxShareRange(txIndexInt)
	if err != nil {
		return ShareProof{}, err
	}

	namespace := getTxNamespace(txs[txIndex])
	return NewShareInclusionProof(dataSquare, namespace, shareRange)
}

func getTxNamespace(tx []byte) (ns share.Namespace) {
	_, isBlobTx, _ := blobtx.UnmarshalBlobTx(tx)
	if isBlobTx {
		return share.PayForBlobNamespace
	}
	return share.TxNamespace
}

// NewShareInclusionProof takes an ODS, extends it, then
// returns an NMT inclusion proof for a set of shares
// belonging to the same namespace to the data root.
// Expects the share range to be pre-validated.
func NewShareInclusionProof(
	dataSquare square.Square,
	namespace share.Namespace,
	shareRange share.Range,
) (ShareProof, error) {
	eds, err := da.ExtendShares(share.ToBytes(dataSquare))
	if err != nil {
		return ShareProof{}, err
	}
	return NewShareInclusionProofFromEDS(eds, namespace, shareRange)
}

// NewShareInclusionProofFromEDS takes an extended data square,
// and returns an NMT inclusion proof for a set of shares
// belonging to the same namespace to the data root.
// Expects the share range to be pre-validated.
func NewShareInclusionProofFromEDS(
	eds *rsmt2d.ExtendedDataSquare,
	namespace share.Namespace,
	shareRange share.Range,
) (ShareProof, error) {
	squareSize := square.Size(len(eds.FlattenedODS()))
	startRow := shareRange.Start / squareSize
	endRow := (shareRange.End - 1) / squareSize
	startLeaf := shareRange.Start % squareSize
	endLeaf := (shareRange.End - 1) % squareSize

	edsRowRoots, err := eds.RowRoots()
	if err != nil {
		return ShareProof{}, err
	}

	edsColRoots, err := eds.ColRoots()
	if err != nil {
		return ShareProof{}, err
	}

	// create the binary merkle inclusion proof for all the square rows to the data root
	_, allProofs := merkle.ProofsFromByteSlices(append(edsRowRoots, edsColRoots...))
	rowProofs := make([]*Proof, endRow-startRow+1)
	rowRoots := make([][]byte, endRow-startRow+1)
	for i := startRow; i <= endRow; i++ {
		rowProofs[i-startRow] = &Proof{
			Total:    allProofs[i].Total,
			Index:    allProofs[i].Index,
			LeafHash: allProofs[i].LeafHash,
			Aunts:    allProofs[i].Aunts,
		}
		rowRoots[i-startRow] = edsRowRoots[i]
	}

	// get the extended rows containing the shares.
	rows := make([][]share.Share, endRow-startRow+1)
	for i := startRow; i <= endRow; i++ {
		shares, err := share.FromBytes(eds.Row(uint(i)))
		if err != nil {
			return ShareProof{}, err
		}
		rows[i-startRow] = shares
	}

	shareProofs, rawShares, err := CreateShareToRowRootProofs(squareSize, rows, rowRoots, startLeaf, endLeaf)
	if err != nil {
		return ShareProof{}, err
	}
	return ShareProof{
		RowProof: &RowProof{
			RowRoots: rowRoots,
			Proofs:   rowProofs,
			StartRow: uint32(startRow),
			EndRow:   uint32(endRow),
		},
		Data:             rawShares,
		ShareProofs:      shareProofs,
		NamespaceId:      namespace.ID(),
		NamespaceVersion: uint32(namespace.Version()),
	}, nil
}

func safeConvertUint64ToInt(val uint64) (int, error) {
	if val > math.MaxInt {
		return 0, fmt.Errorf("value %d is too large to convert to int", val)
	}
	return int(val), nil
}

// CreateShareToRowRootProofs takes a set of shares and their corresponding row roots, and generates
// an NMT inclusion proof of a set of shares, defined by startLeaf and endLeaf, to their corresponding row roots.
func CreateShareToRowRootProofs(squareSize int, rowShares [][]share.Share, rowRoots [][]byte, startLeaf, endLeaf int) ([]*NMTProof, [][]byte, error) {
	shareProofs := make([]*NMTProof, 0, len(rowRoots))
	var rawShares [][]byte
	for i, row := range rowShares {
		// create an nmt to generate a proof.
		// we have to re-create the tree as the eds one is not accessible.
		tree := wrapper.NewErasuredNamespacedMerkleTree(uint64(squareSize), uint(i))
		for _, share := range row {
			err := tree.Push(
				share.ToBytes(),
			)
			if err != nil {
				return nil, nil, err
			}
		}

		// make sure that the generated root is the same as the eds row root.
		root, err := tree.Root()
		if err != nil {
			return nil, nil, err
		}
		if !bytes.Equal(rowRoots[i], root) {
			return nil, nil, errors.New("eds row root is different than tree root")
		}

		startLeafPos := startLeaf
		endLeafPos := endLeaf

		// if this is not the first row, then start with the first leaf
		if i > 0 {
			startLeafPos = 0
		}
		// if this is not the last row, then select for the rest of the row
		if i != (len(rowShares) - 1) {
			endLeafPos = squareSize - 1
		}

		rawShares = append(rawShares, share.ToBytes(row[startLeafPos:endLeafPos+1])...)
		proof, err := tree.ProveRange(startLeafPos, endLeafPos+1)
		if err != nil {
			return nil, nil, err
		}

		shareProofs = append(shareProofs, &NMTProof{
			Start:    int32(proof.Start()),
			End:      int32(proof.End()),
			Nodes:    proof.Nodes(),
			LeafHash: proof.LeafHash(),
		})
	}
	return shareProofs, rawShares, nil
}

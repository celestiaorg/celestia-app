package proof

import (
	"bytes"
	"errors"
	"fmt"

	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	coretypes "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/celestiaorg/rsmt2d"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/da"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/celestiaorg/celestia-app/pkg/wrapper"
	"github.com/tendermint/tendermint/crypto/merkle"
	"github.com/tendermint/tendermint/types"
)

// NewTxInclusionProof returns a new share inclusion proof for the given
// transaction index.
func NewTxInclusionProof(txs [][]byte, txIndex, appVersion uint64) (types.ShareProof, error) {
	if txIndex >= uint64(len(txs)) {
		return types.ShareProof{}, fmt.Errorf("txIndex %d out of bounds", txIndex)
	}

	builder, err := square.NewBuilder(appconsts.SquareSizeUpperBound(appVersion), appVersion, txs...)
	if err != nil {
		return types.ShareProof{}, err
	}

	dataSquare, err := builder.Export()
	if err != nil {
		return types.ShareProof{}, err
	}

	shareRange, err := builder.FindTxShareRange(int(txIndex))
	if err != nil {
		return types.ShareProof{}, err
	}

	namespace := getTxNamespace(txs[txIndex])
	return NewShareInclusionProof(dataSquare, namespace, shareRange)
}

func getTxNamespace(tx []byte) (ns appns.Namespace) {
	_, isBlobTx := types.UnmarshalBlobTx(tx)
	if isBlobTx {
		return appns.PayForBlobNamespace
	}
	return appns.TxNamespace
}

// NewShareInclusionProof takes an ODS, extends it, then
// returns an NMT inclusion proof for a set of shares
// belonging to the same namespace to the data root.
// Expects the share range to be pre-validated.
func NewShareInclusionProof(
	dataSquare square.Square,
	namespace appns.Namespace,
	shareRange shares.Range,
) (types.ShareProof, error) {
	eds, err := da.ExtendShares(shares.ToBytes(dataSquare))
	if err != nil {
		return types.ShareProof{}, err
	}
	return NewShareInclusionProofFromEDS(eds, namespace, shareRange)
}

// NewShareInclusionProofFromEDS takes an extended data square,
// and returns an NMT inclusion proof for a set of shares
// belonging to the same namespace to the data root.
// Expects the share range to be pre-validated.
func NewShareInclusionProofFromEDS(
	eds *rsmt2d.ExtendedDataSquare,
	namespace appns.Namespace,
	shareRange shares.Range,
) (types.ShareProof, error) {
	squareSize := square.Size(len(eds.FlattenedODS()))
	startRow := shareRange.Start / squareSize
	endRow := (shareRange.End - 1) / squareSize
	startLeaf := shareRange.Start % squareSize
	endLeaf := (shareRange.End - 1) % squareSize

	edsRowRoots, err := eds.RowRoots()
	if err != nil {
		return types.ShareProof{}, err
	}

	edsColRoots, err := eds.ColRoots()
	if err != nil {
		return types.ShareProof{}, err
	}

	// create the binary merkle inclusion proof for all the square rows to the data root
	_, allProofs := merkle.ProofsFromByteSlices(append(edsRowRoots, edsColRoots...))
	rowProofs := make([]*merkle.Proof, endRow-startRow+1)
	rowRoots := make([]tmbytes.HexBytes, endRow-startRow+1)
	bzRowRoots := make([][]byte, endRow-startRow+1)
	for i := startRow; i <= endRow; i++ {
		rowProofs[i-startRow] = allProofs[i]
		rowRoots[i-startRow] = edsRowRoots[i]
		bzRowRoots[i-startRow] = edsRowRoots[i]
	}

	// get the extended rows containing the shares.
	rows := make([][]shares.Share, endRow-startRow+1)
	for i := startRow; i <= endRow; i++ {
		shares, err := shares.FromBytes(eds.Row(uint(i)))
		if err != nil {
			return types.ShareProof{}, err
		}
		rows[i-startRow] = shares
	}

	shareProofs, rawShares, err := CreateShareToRowRootProofs(squareSize, rows, bzRowRoots, startLeaf, endLeaf)
	if err != nil {
		return types.ShareProof{}, err
	}
	return types.ShareProof{
		RowProof: types.RowProof{
			RowRoots: rowRoots,
			Proofs:   rowProofs,
			StartRow: uint32(startRow),
			EndRow:   uint32(endRow),
		},
		Data:             rawShares,
		ShareProofs:      shareProofs,
		NamespaceID:      namespace.ID,
		NamespaceVersion: uint32(namespace.Version),
	}, nil
}

// CreateShareToRowRootProofs takes a set of shares and their corresponding row roots, and generates
// an NMT inclusion proof of a set of shares, defined by startLeaf and endLeaf, to their corresponding row roots.
func CreateShareToRowRootProofs(squareSize int, rowShares [][]shares.Share, rowRoots [][]byte, startLeaf, endLeaf int) ([]*coretypes.NMTProof, [][]byte, error) {
	shareProofs := make([]*coretypes.NMTProof, 0, len(rowRoots))
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

		rawShares = append(rawShares, shares.ToBytes(row[startLeafPos:endLeafPos+1])...)
		proof, err := tree.ProveRange(startLeafPos, endLeafPos+1)
		if err != nil {
			return nil, nil, err
		}

		shareProofs = append(shareProofs, &coretypes.NMTProof{
			Start:    int32(proof.Start()),
			End:      int32(proof.End()),
			Nodes:    proof.Nodes(),
			LeafHash: proof.LeafHash(),
		})
	}
	return shareProofs, rawShares, nil
}

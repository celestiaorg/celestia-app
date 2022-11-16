package prove

import (
	"errors"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/wrapper"
	"github.com/celestiaorg/rsmt2d"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/types"
)

// TxInclusion uses the provided block data to progressively generate rows
// of a data square, and then using those shares to creates nmt inclusion proofs
// It is possible that a transaction spans more than one row. In that case, we
// have to return more than one proof.
func TxInclusion(codec rsmt2d.Codec, data types.Data, txIndex uint64) (types.TxProof, error) {
	// calculate the index of the shares that contain the tx
	startPos, endPos, err := txSharePosition(data.Txs, txIndex)
	if err != nil {
		return types.TxProof{}, err
	}

	// use the index of the shares and the square size to determine the row that
	// contains the tx we need to prove
	startRow := startPos / data.OriginalSquareSize
	endRow := endPos / data.OriginalSquareSize
	startLeaf := startPos % data.OriginalSquareSize
	endLeaf := endPos % data.OriginalSquareSize

	rowShares, err := genRowShares(codec, data, startRow, endRow)
	if err != nil {
		return types.TxProof{}, err
	}

	var proofs []*tmproto.NMTProof  //nolint:prealloc // rarely will this contain more than a single proof
	var rawShares [][]byte          //nolint:prealloc // rarely will this contain more than a single share
	var rowRoots []tmbytes.HexBytes //nolint:prealloc // rarely will this contain more than a single root
	for i, row := range rowShares {
		// create an nmt to use to generate a proof
		tree := wrapper.NewErasuredNamespacedMerkleTree(data.OriginalSquareSize, uint(i))
		for _, share := range row {
			tree.Push(
				share,
			)
		}

		startLeafPos := startLeaf
		endLeafPos := endLeaf

		// if this is not the first row, then start with the first leaf
		if i > 0 {
			startLeafPos = 0
		}
		// if this is not the last row, then select for the rest of the row
		if i != (len(rowShares) - 1) {
			endLeafPos = data.OriginalSquareSize - 1
		}

		rawShares = append(rawShares, shares.ToBytes(row[startLeafPos:endLeafPos+1])...)
		proof, err := tree.Tree().ProveRange(int(startLeafPos), int(endLeafPos+1))
		if err != nil {
			return types.TxProof{}, err
		}

		proofs = append(proofs, &tmproto.NMTProof{
			Start:    int32(proof.Start()),
			End:      int32(proof.End()),
			Nodes:    proof.Nodes(),
			LeafHash: proof.LeafHash(),
		})

		// we don't store the data availability header anywhere, so we
		// regenerate the roots to each row
		rowRoots = append(rowRoots, tree.Root())
	}

	return types.TxProof{
		RowRoots: rowRoots,
		Data:     rawShares,
		Proofs:   proofs,
	}, nil
}

// txSharePosition returns the start and end positions for the shares that
// include a given txIndex. Returns an error if index is greater than the length
// of txs.
func txSharePosition(txs types.Txs, txIndex uint64) (startSharePos, endSharePos uint64, err error) {
	if txIndex >= uint64(len(txs)) {
		return startSharePos, endSharePos, errors.New("transaction index is greater than the number of txs")
	}

	prevTxTotalLen := 0
	for i := uint64(0); i < txIndex; i++ {
		txLen := len(txs[i])
		prevTxTotalLen += (shares.DelimLen(uint64(txLen)) + txLen)
	}

	currentTxLen := len(txs[txIndex])
	currentTxTotalLen := shares.DelimLen(uint64(currentTxLen)) + currentTxLen
	endOfCurrentTxLen := prevTxTotalLen + currentTxTotalLen

	startSharePos = txShareIndex(prevTxTotalLen)
	endSharePos = txShareIndex(endOfCurrentTxLen)
	return startSharePos, endSharePos, nil
}

// txShareIndex returns the index of the compact share that would contain
// transactions with totalTxLen
func txShareIndex(totalTxLen int) (index uint64) {
	if totalTxLen <= appconsts.FirstCompactShareContentSize {
		return 0
	}

	index++
	totalTxLen -= appconsts.FirstCompactShareContentSize

	for totalTxLen > appconsts.ContinuationCompactShareContentSize {
		index++
		totalTxLen -= appconsts.ContinuationCompactShareContentSize
	}
	return index
}

// genRowShares progessively generates data square rows from block data
func genRowShares(codec rsmt2d.Codec, data types.Data, startRow, endRow uint64) ([][]shares.Share, error) {
	if endRow > data.OriginalSquareSize {
		return nil, errors.New("cannot generate row shares past the original square size")
	}
	origRowShares := splitIntoRows(
		data.OriginalSquareSize,
		genOrigRowShares(data, startRow, endRow),
	)

	encodedRowShares := make([][]shares.Share, len(origRowShares))
	for i, row := range origRowShares {
		encRow, err := codec.Encode(shares.ToBytes(row))
		if err != nil {
			panic(err)
		}
		encodedRowShares[i] = append(
			append(
				make([]shares.Share, 0, len(row)+len(encRow)),
				row...,
			), shares.FromBytes(encRow)...,
		)
	}

	return encodedRowShares, nil
}

// genOrigRowShares progressively generates data square rows for the original
// data square, meaning the rows only half the full square length, as there is
// not erasure data
func genOrigRowShares(data types.Data, startRow, endRow uint64) []shares.Share {
	wantLen := (endRow + 1) * data.OriginalSquareSize
	startPos := startRow * data.OriginalSquareSize

	rawShares := shares.SplitTxs(data.Txs)
	// return if we have enough shares
	if uint64(len(rawShares)) >= wantLen {
		return rawShares[startPos:wantLen]
	}

	evdShares, err := shares.SplitEvidence(data.Evidence.Evidence)
	if err != nil {
		panic(err)
	}

	rawShares = append(rawShares, evdShares...)
	if uint64(len(rawShares)) >= wantLen {
		return rawShares[startPos:wantLen]
	}

	for _, m := range data.Messages.MessagesList {
		msgShares, err := shares.SplitMessages(0, nil, []types.Message{m}, false)
		if err != nil {
			panic(err)
		}

		rawShares = append(rawShares, msgShares...)

		// return if we have enough shares
		if uint64(len(rawShares)) >= wantLen {
			return rawShares[startPos:wantLen]
		}
	}

	tailShares := shares.TailPaddingShares(int(wantLen) - len(rawShares))
	rawShares = append(rawShares, tailShares...)

	return rawShares[startPos:wantLen]
}

// splitIntoRows splits shares into rows of a particular square size
func splitIntoRows(squareSize uint64, s []shares.Share) [][]shares.Share {
	rowCount := uint64(len(s)) / squareSize
	rows := make([][]shares.Share, rowCount)
	for i := uint64(0); i < rowCount; i++ {
		rows[i] = s[i*squareSize : (i+1)*squareSize]
	}
	return rows
}

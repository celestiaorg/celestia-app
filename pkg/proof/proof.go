package proof

import (
	"bytes"
	"errors"
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/wrapper"
	blobmodule "github.com/celestiaorg/celestia-app/x/blob"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/celestiaorg/rsmt2d"
	"github.com/tendermint/tendermint/crypto/merkle"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/types"
)

// TxInclusion uses the provided block data to progressively generate rows
// of a data square, and then using those shares to creates nmt inclusion proofs.
// It is possible that a transaction spans more than one row. In that case, we
// have to return more than one proof.
func TxInclusion(codec rsmt2d.Codec, data types.Data, txIndex uint64) (types.TxProof, error) {
	// calculate the index of the shares that contain the tx
	startPos, endPos, err := TxSharePosition(data.Txs, txIndex)
	if err != nil {
		return types.TxProof{}, err
	}

	// use the index of the shares and the square size to determine the row that
	// contains the tx we need to prove
	startRow := startPos / data.SquareSize
	endRow := endPos / data.SquareSize
	startLeaf := startPos % data.SquareSize
	endLeaf := endPos % data.SquareSize

	rowShares, err := genRowShares(codec, data, startRow, endRow)
	if err != nil {
		return types.TxProof{}, err
	}

	var proofs []*tmproto.NMTProof  //nolint:prealloc // rarely will this contain more than a single proof
	var rawShares [][]byte          //nolint:prealloc // rarely will this contain more than a single share
	var rowRoots []tmbytes.HexBytes //nolint:prealloc // rarely will this contain more than a single root
	for i, row := range rowShares {
		// create an nmt to use to generate a proof
		tree := wrapper.NewErasuredNamespacedMerkleTree(data.SquareSize, uint(i))
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
			endLeafPos = data.SquareSize - 1
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

// TxSharePosition returns the start and end positions for the shares that
// include a given txIndex. Returns an error if index is greater than the length
// of txs.
func TxSharePosition(txs types.Txs, txIndex uint64) (startSharePos, endSharePos uint64, err error) {
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

// BlobShareRange returns the start and end positions for the shares
// where a given blob, referenced by its wrapped PFB transaction, was published at.
// Note: only supports transactions containing a single blob.
func BlobShareRange(tx types.Tx) (beginShare uint64, endShare uint64, err error) {
	indexWrappedTx, isIndexWrapped := types.UnmarshalIndexWrapper(tx)
	if !isIndexWrapped {
		return beginShare, endShare, fmt.Errorf("not an index wrapped tx")
	}

	encCfg := encoding.MakeConfig(blobmodule.AppModuleBasic{})
	decodedTx, err := encCfg.TxConfig.TxDecoder()(indexWrappedTx.Tx)
	if err != nil {
		return beginShare, endShare, err
	}

	if len(decodedTx.GetMsgs()) == 0 {
		return beginShare, endShare, fmt.Errorf("PayForBlobs contains no messages")
	}

	if len(decodedTx.GetMsgs()) > 1 {
		return beginShare, endShare, fmt.Errorf("PayForBlobs contains multiple messages and this is not currently supported")
	}

	if sdk.MsgTypeURL(decodedTx.GetMsgs()[0]) != blobtypes.URLMsgPayForBlobs {
		return beginShare, endShare, fmt.Errorf("msg is not a MsgPayForBlobs")
	}

	pfb, ok := decodedTx.GetMsgs()[0].(*blobtypes.MsgPayForBlobs)
	if !ok {
		return beginShare, endShare, fmt.Errorf("unable to decode PayForBlob")
	}

	if err = pfb.ValidateBasic(); err != nil {
		return beginShare, endShare, err
	}

	beginShare = uint64(indexWrappedTx.ShareIndexes[0])
	sharesUsed := shares.SparseSharesNeeded(pfb.BlobSizes[0])
	return beginShare, beginShare + uint64(sharesUsed) - 1, nil
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
	if endRow > data.SquareSize {
		return nil, errors.New("cannot generate row shares past the original square size")
	}
	origRowShares := splitIntoRows(
		data.SquareSize,
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
	wantLen := (endRow + 1) * data.SquareSize
	startPos := startRow * data.SquareSize

	rawTxShares, pfbTxShares := shares.SplitTxs(data.Txs)
	rawShares := append(rawTxShares, pfbTxShares...)
	// return if we have enough shares
	if uint64(len(rawShares)) >= wantLen {
		return rawShares[startPos:wantLen]
	}

	for _, blob := range data.Blobs {
		blobShares, err := shares.SplitBlobs(0, nil, []types.Blob{blob}, false)
		if err != nil {
			panic(err)
		}

		// TODO: does this need to account for padding between compact shares
		// and the first blob?
		// https://github.com/celestiaorg/celestia-app/issues/1226
		rawShares = append(rawShares, blobShares...)

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

// GenerateSharesInclusionProof generates an nmt inclusion proof for a set of shares to the data root.
// Expects the share range to be pre-validated.
// Note: only supports inclusion proofs for shares belonging to the same namespace.
func GenerateSharesInclusionProof(
	allRawShares []shares.Share,
	squareSize uint64,
	namespaceID namespace.ID,
	startShare uint64,
	endShare uint64,
) (types.SharesProof, error) {
	startRow := startShare / squareSize
	endRow := endShare / squareSize
	startLeaf := startShare % squareSize
	endLeaf := endShare % squareSize

	eds, err := da.ExtendShares(squareSize, shares.ToBytes(allRawShares))
	if err != nil {
		return types.SharesProof{}, err
	}

	edsRowRoots := eds.RowRoots()

	// create the binary merkle inclusion proof for all the square rows to the data root
	_, allProofs := merkle.ProofsFromByteSlices(append(edsRowRoots, eds.ColRoots()...))
	rowsProofs := make([]*merkle.Proof, endRow-startRow+1)
	rowsRoots := make([]tmbytes.HexBytes, endRow-startRow+1)
	for i := startRow; i <= endRow; i++ {
		rowsProofs[i-startRow] = allProofs[i]
		rowsRoots[i-startRow] = edsRowRoots[i]
	}

	// get the extended rows containing the shares.
	rows := make([][]shares.Share, endRow-startRow+1)
	for i := startRow; i <= endRow; i++ {
		rows[i-startRow] = shares.FromBytes(eds.Row(uint(i)))
	}

	var sharesProofs []*tmproto.NMTProof //nolint:prealloc
	var rawShares [][]byte
	for i, row := range rows {
		// create an nmt to generate a proof.
		// we have to re-create the tree as the eds one is not accessible.
		tree := wrapper.NewErasuredNamespacedMerkleTree(squareSize, uint(i))
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
		if i != (len(rows) - 1) {
			endLeafPos = squareSize - 1
		}

		rawShares = append(rawShares, shares.ToBytes(row[startLeafPos:endLeafPos+1])...)
		proof, err := tree.Tree().ProveRange(int(startLeafPos), int(endLeafPos+1))
		if err != nil {
			return types.SharesProof{}, err
		}

		sharesProofs = append(sharesProofs, &tmproto.NMTProof{
			Start:    int32(proof.Start()),
			End:      int32(proof.End()),
			Nodes:    proof.Nodes(),
			LeafHash: proof.LeafHash(),
		})

		// make sure that the generated root is the same as the eds row root.
		if !bytes.Equal(rowsRoots[i].Bytes(), tree.Root()) {
			return types.SharesProof{}, errors.New("eds row root is different than tree root")
		}
	}

	return types.SharesProof{
		RowsProof: types.RowsProof{
			RowsRoots: rowsRoots,
			Proofs:    rowsProofs,
			StartRow:  uint32(startRow),
			EndRow:    uint32(endRow),
		},
		Data:         rawShares,
		SharesProofs: sharesProofs,
		NamespaceID:  namespaceID,
	}, nil
}

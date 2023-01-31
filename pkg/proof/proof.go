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

// NewTxInclusionProof returns a new share inclusion proof for the given
// transaction index.
func NewTxInclusionProof(codec rsmt2d.Codec, data types.Data, txIndex uint64) (types.ShareProof, error) {
	rawShares, err := shares.Split(data, true)
	if err != nil {
		return types.ShareProof{}, err
	}

	startShare, endShare, err := TxSharePosition(data, txIndex)
	if err != nil {
		return types.ShareProof{}, err
	}

	namespace := getTxNamespace(data.Txs[txIndex])
	return NewShareInclusionProof(rawShares, data.SquareSize, namespace, startShare, endShare)
}

func getTxNamespace(tx types.Tx) (ns namespace.ID) {
	_, isIndexWrapper := types.UnmarshalIndexWrapper(tx)
	if isIndexWrapper {
		return appconsts.PayForBlobNamespaceID
	}
	return appconsts.TxNamespaceID
}

// TxSharePosition returns the start and end positions for the shares that
// include a given txIndex. Returns an error if index is greater than the length
// of txs.
func TxSharePosition(data types.Data, txIndex uint64) (startShare uint64, endShare uint64, err error) {
	if int(txIndex) >= len(data.Txs) {
		return 0, 0, errors.New("transaction index is greater than the number of txs")
	}

	_, _, shareRanges := shares.SplitTxs(data.Txs)
	shareRange := shareRanges[data.Txs[txIndex].Key()]

	return uint64(shareRange.Start), uint64(shareRange.End), nil
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
		return beginShare, endShare, fmt.Errorf("expected msg type %s, but got %s instead", blobtypes.URLMsgPayForBlobs, sdk.MsgTypeURL(decodedTx.GetMsgs()[0]))
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

// NewShareInclusionProof returns an NMT inclusion proof for a set of shares to the data root.
// Expects the share range to be pre-validated.
// Note: only supports inclusion proofs for shares belonging to the same namespace.
func NewShareInclusionProof(
	allRawShares []shares.Share,
	squareSize uint64,
	namespaceID namespace.ID,
	startShare uint64,
	endShare uint64,
) (types.ShareProof, error) {
	startRow := startShare / squareSize
	endRow := endShare / squareSize
	startLeaf := startShare % squareSize
	endLeaf := endShare % squareSize

	eds, err := da.ExtendShares(squareSize, shares.ToBytes(allRawShares))
	if err != nil {
		return types.ShareProof{}, err
	}

	edsRowRoots := eds.RowRoots()

	// create the binary merkle inclusion proof for all the square rows to the data root
	_, allProofs := merkle.ProofsFromByteSlices(append(edsRowRoots, eds.ColRoots()...))
	rowProofs := make([]*merkle.Proof, endRow-startRow+1)
	rowRoots := make([]tmbytes.HexBytes, endRow-startRow+1)
	for i := startRow; i <= endRow; i++ {
		rowProofs[i-startRow] = allProofs[i]
		rowRoots[i-startRow] = edsRowRoots[i]
	}

	// get the extended rows containing the shares.
	rows := make([][]shares.Share, endRow-startRow+1)
	for i := startRow; i <= endRow; i++ {
		rows[i-startRow] = shares.FromBytes(eds.Row(uint(i)))
	}

	var shareProofs []*tmproto.NMTProof //nolint:prealloc
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
			return types.ShareProof{}, err
		}

		shareProofs = append(shareProofs, &tmproto.NMTProof{
			Start:    int32(proof.Start()),
			End:      int32(proof.End()),
			Nodes:    proof.Nodes(),
			LeafHash: proof.LeafHash(),
		})

		// make sure that the generated root is the same as the eds row root.
		if !bytes.Equal(rowRoots[i].Bytes(), tree.Root()) {
			return types.ShareProof{}, errors.New("eds row root is different than tree root")
		}
	}

	return types.ShareProof{
		RowProof: types.RowProof{
			RowRoots: rowRoots,
			Proofs:   rowProofs,
			StartRow: uint32(startRow),
			EndRow:   uint32(endRow),
		},
		Data:        rawShares,
		ShareProofs: shareProofs,
		NamespaceID: namespaceID,
	}, nil
}

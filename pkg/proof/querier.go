package proof

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/nmt/namespace"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/types"
)

const TxInclusionQueryPath = "txInclusionProof"

// Querier defines the logic performed when the ABCI client using the Query
// method with the custom prove.QueryPath. The index of the transaction being
// proved must be appended to the path. The marshalled bytes of the transaction
// proof (tmproto.TxProof) are returned.
//
// example path for proving the third transaction in that block:
// custom/txInclusionProof/3
func QueryTxInclusionProof(_ sdk.Context, path []string, req abci.RequestQuery) ([]byte, error) {
	// parse the index from the path
	if len(path) != 1 {
		return nil, fmt.Errorf("expected query path length: 1 actual: %d ", len(path))
	}
	index, err := strconv.ParseInt(path[0], 10, 64)
	if err != nil {
		return nil, err
	}

	// unmarshal the block data that is passed from the ABCI client
	pbb := new(tmproto.Block)
	err = pbb.Unmarshal(req.Data)
	if err != nil {
		return nil, fmt.Errorf("error reading block: %w", err)
	}
	data, err := types.DataFromProto(&pbb.Data)
	if err != nil {
		panic(fmt.Errorf("error from proto block: %w", err))
	}

	// create and marshal the tx inclusion proof, which we return in the form of []byte
	txProof, err := TxInclusion(appconsts.DefaultCodec(), data, uint64(index))
	if err != nil {
		return nil, err
	}
	pTxProof := txProof.ToProto()
	rawTxProof, err := pTxProof.Marshal()
	if err != nil {
		return nil, err
	}

	return rawTxProof, nil
}

const ShareInclusionQueryPath = "shareInclusionProof"

// QueryShareInclusionProof defines the logic performed when querying for the
// inclusion proofs of a set of shares to the data root. The share range should
// be appended to the path. Example path for proving the set of shares [3, 5]:
// custom/shareInclusionProof/3/5
func QueryShareInclusionProof(_ sdk.Context, path []string, req abci.RequestQuery) ([]byte, error) {
	// parse the share range from the path
	if len(path) != 2 {
		return nil, fmt.Errorf("expected query path length: 2 actual: %d ", len(path))
	}
	beginShare, err := strconv.ParseInt(path[0], 10, 64)
	if err != nil {
		return nil, err
	}
	endShare, err := strconv.ParseInt(path[1], 10, 64)
	if err != nil {
		return nil, err
	}

	// unmarshal the block data that is passed from the ABCI client
	pbb := new(tmproto.Block)
	err = pbb.Unmarshal(req.Data)
	if err != nil {
		return nil, fmt.Errorf("error reading block: %w", err)
	}
	data, err := types.DataFromProto(&pbb.Data)
	if err != nil {
		panic(fmt.Errorf("error from proto block: %w", err))
	}

	rawShares, err := shares.Split(data, true)
	if err != nil {
		return nil, err
	}

	nID, err := ParseNamespaceID(rawShares, beginShare, endShare)
	if err != nil {
		return nil, err
	}

	// create and marshal the shares inclusion proof, which we return in the form of []byte
	txProof, err := GenerateSharesInclusionProof(
		rawShares,
		data.SquareSize,
		nID,
		uint64(beginShare),
		uint64(endShare),
	)
	if err != nil {
		return nil, err
	}
	pTxProof := txProof.ToProto()
	rawTxProof, err := pTxProof.Marshal()
	if err != nil {
		return nil, err
	}

	return rawTxProof, nil
}

// ParseNamespaceID validates the share range, checks if it only contains one namespace and returns
// that namespace ID.
func ParseNamespaceID(rawShares []shares.Share, startShare int64, endShare int64) (namespace.ID, error) {
	if startShare < 0 {
		return nil, fmt.Errorf("start share %d should be positive", startShare)
	}

	if endShare < 0 {
		return nil, fmt.Errorf("end share %d should be positive", endShare)
	}

	if endShare < startShare {
		return nil, fmt.Errorf("end share %d cannot be lower than starting share %d", endShare, startShare)
	}

	if endShare >= int64(len(rawShares)) {
		return nil, fmt.Errorf("end share %d is higher than block shares %d", endShare, len(rawShares))
	}

	nID := rawShares[startShare].NamespaceID()

	for i, n := range rawShares[startShare:endShare] {
		if !bytes.Equal(nID, n.NamespaceID()) {
			return nil, fmt.Errorf("shares range contain different namespaces: %d and %d at index %d", nID, n.NamespaceID(), i)
		}
	}
	return nID, nil
}

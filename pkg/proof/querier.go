package proof

import (
	"bytes"
	"fmt"
	"strconv"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/pkg/square"

	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	"github.com/tendermint/tendermint/types"
)

const TxInclusionQueryPath = "txInclusionProof"

// Querier defines the logic performed when the ABCI client using the Query
// method with the custom prove.QueryPath. The index of the transaction being
// proved must be appended to the path. The marshalled bytes of the transaction
// proof (tmproto.ShareProof) are returned.
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
	if index < 0 {
		return nil, fmt.Errorf("path[0] element: %q produced a negative value: %d", path[0], index)
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
	shareProof, err := NewTxInclusionProof(data.Txs.ToSliceOfBytes(), uint64(index), pbb.Header.Version.App)
	if err != nil {
		return nil, err
	}
	pShareProof := shareProof.ToProto()
	rawShareProof, err := pShareProof.Marshal()
	if err != nil {
		return nil, err
	}

	return rawShareProof, nil
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

	// construct the data square from the block data. As we don't have
	// access to the application's state machine we use the upper bound
	// square size instead of the square size dictated from governance
	dataSquare, err := square.Construct(pbb.Data.Txs, pbb.Header.Version.App, appconsts.SquareSizeUpperBound(pbb.Header.Version.App))
	if err != nil {
		return nil, err
	}

	nID, err := ParseNamespace(dataSquare, int(beginShare), int(endShare))
	if err != nil {
		return nil, err
	}

	// create and marshal the share inclusion proof, which we return in the form of []byte
	shareProof, err := NewShareInclusionProof(
		dataSquare,
		nID,
		shares.NewRange(int(beginShare), int(endShare)),
	)
	if err != nil {
		return nil, err
	}
	pShareProof := shareProof.ToProto()
	rawShareProof, err := pShareProof.Marshal()
	if err != nil {
		return nil, err
	}

	return rawShareProof, nil
}

// ParseNamespace validates the share range, checks if it only contains one namespace and returns
// that namespace ID.
// The provided range, defined by startShare and endShare, is end-exclusive.
func ParseNamespace(rawShares []shares.Share, startShare, endShare int) (appns.Namespace, error) {
	if startShare < 0 {
		return appns.Namespace{}, fmt.Errorf("start share %d should be positive", startShare)
	}

	if endShare <= startShare {
		return appns.Namespace{}, fmt.Errorf("end share %d cannot be lower or equal to the starting share %d", endShare, startShare)
	}

	if endShare > len(rawShares) {
		return appns.Namespace{}, fmt.Errorf("end share %d is higher than block shares %d", endShare, len(rawShares))
	}

	startShareNs, err := rawShares[startShare].Namespace()
	if err != nil {
		return appns.Namespace{}, err
	}

	for i, share := range rawShares[startShare:endShare] {
		ns, err := share.Namespace()
		if err != nil {
			return appns.Namespace{}, err
		}
		if !bytes.Equal(startShareNs.Bytes(), ns.Bytes()) {
			return appns.Namespace{}, fmt.Errorf("shares range contain different namespaces at index %d: %v and %v ", i, startShareNs, ns)
		}
	}
	return startShareNs, nil
}

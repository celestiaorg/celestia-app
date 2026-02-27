package proof

import (
	"bytes"
	"fmt"
	"math"
	"strconv"

	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/celestiaorg/go-square/v4"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const TxInclusionQueryPath = "txInclusionProof"

// NewQueryTxInclusionProofHandler returns a Querier that handles tx inclusion proof queries.
// The handler parameter is used for PayForFibre transaction identification during square construction.
//
// example path for proving the third transaction in that block:
// custom/txInclusionProof/3
func NewQueryTxInclusionProofHandler(handler square.PayForFibreHandler) sdk.Querier {
	return func(_ sdk.Context, path []string, req *abci.RequestQuery) ([]byte, error) {
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
		shareProof, err := NewTxInclusionProof(data.Txs.ToSliceOfBytes(), uint64(index), handler)
		if err != nil {
			return nil, err
		}

		rawShareProof, err := shareProof.Marshal()
		if err != nil {
			return nil, err
		}

		return rawShareProof, nil
	}
}

const ShareInclusionQueryPath = "shareInclusionProof"

// NewQueryShareInclusionProofHandler returns a Querier that handles share inclusion proof queries.
// The handler parameter is used for PayForFibre transaction identification during square construction.
// Example path for proving the set of shares [3, 5]:
// custom/shareInclusionProof/3/5
func NewQueryShareInclusionProofHandler(handler square.PayForFibreHandler) sdk.Querier {
	return func(_ sdk.Context, path []string, req *abci.RequestQuery) ([]byte, error) {
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
		dataSquare, err := square.Construct(pbb.Data.Txs, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold, handler)
		if err != nil {
			return nil, err
		}

		begin, err := safeConvertInt64ToInt(beginShare)
		if err != nil {
			return nil, err
		}
		end, err := safeConvertInt64ToInt(endShare)
		if err != nil {
			return nil, err
		}

		nID, err := ParseNamespace(dataSquare, begin, end)
		if err != nil {
			return nil, err
		}

		shareRange := share.NewRange(begin, end)
		// create and marshal the share inclusion proof, which we return in the form of []byte
		shareProof, err := NewShareInclusionProof(dataSquare, nID, shareRange)
		if err != nil {
			return nil, err
		}

		rawShareProof, err := shareProof.Marshal()
		if err != nil {
			return nil, err
		}

		return rawShareProof, nil
	}
}

// ParseNamespace validates the share range, checks if it only contains one namespace and returns
// that namespace ID.
// The provided range, defined by startShare and endShare, is end-exclusive.
func ParseNamespace(rawShares []share.Share, startShare, endShare int) (share.Namespace, error) {
	if startShare < 0 {
		return share.Namespace{}, fmt.Errorf("start share %d should be positive", startShare)
	}

	if endShare < 0 {
		return share.Namespace{}, fmt.Errorf("end share %d should be positive", endShare)
	}

	if endShare <= startShare {
		return share.Namespace{}, fmt.Errorf("end share %d cannot be lower or equal to the starting share %d", endShare, startShare)
	}

	if endShare > len(rawShares) {
		return share.Namespace{}, fmt.Errorf("end share %d is higher than block shares %d", endShare, len(rawShares))
	}

	startShareNs := rawShares[startShare].Namespace()
	for i, sh := range rawShares[startShare:endShare] {
		ns := sh.Namespace()
		if !bytes.Equal(startShareNs.Bytes(), ns.Bytes()) {
			return share.Namespace{}, fmt.Errorf("shares range contain different namespaces at index %d: %v and %v ", i, startShareNs, ns)
		}
	}
	return startShareNs, nil
}

func safeConvertInt64ToInt(x int64) (int, error) {
	if x < math.MinInt {
		return 0, fmt.Errorf("value %d is too small to be converted to int", x)
	}
	if x > math.MaxInt {
		return 0, fmt.Errorf("value %d is too large to be converted to int", x)
	}
	return int(x), nil
}

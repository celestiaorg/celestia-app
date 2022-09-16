package prove

import (
	"fmt"
	"strconv"

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

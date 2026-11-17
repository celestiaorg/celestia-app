package app_test

import (
	"context"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/app"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	abci "github.com/cometbft/cometbft/abci/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	"github.com/stretchr/testify/require"
)

// TestNestedABCIQueryRejectsProofPaths checks that a proof query sent to
// Query inside the gRPC ABCIQuery method (as CometBFT abci_query can do) is
// rejected, while a direct proof query (as CometBFT's own tx and prove_shares
// routes send) still reaches the proof querier.
func TestNestedABCIQueryRejectsProofPaths(t *testing.T) {
	testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	testApp.RegisterTendermintService(client.Context{})

	block := tmproto.Block{Data: tmproto.Data{Txs: [][]byte{{1}}}}
	blockBytes, err := block.Marshal()
	require.NoError(t, err)

	inner := &cmtservice.ABCIQueryRequest{Path: "custom/txInclusionProof/0", Data: blockBytes}
	innerBytes, err := inner.Marshal()
	require.NoError(t, err)

	res, err := testApp.Query(context.Background(), &abci.RequestQuery{
		Path: "/cosmos.base.tendermint.v1beta1.Service/ABCIQuery",
		Data: innerBytes,
	})
	if err != nil {
		require.Contains(t, err.Error(), "not served")
	} else {
		require.NotZero(t, res.Code, "nested proof query must fail")
		require.Contains(t, res.Log, "not served")
	}

	res, err = testApp.Query(context.Background(), &abci.RequestQuery{
		Path: "custom/txInclusionProof/0",
		Data: blockBytes,
	})
	require.NoError(t, err)
	require.Zero(t, res.Code, res.Log)
	require.NotEmpty(t, res.Value)
}

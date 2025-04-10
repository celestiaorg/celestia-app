package app_test

import (
	"context"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coregrpc "github.com/tendermint/tendermint/rpc/grpc"
	"testing"
)

func TestEnsureBlockAPI(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping app/test/block_api in short mode.")
	}

	// test setup: create a test chain
	cfg := testnode.DefaultConfig()
	cctx, _, _ := testnode.NewNetwork(t, cfg)

	// create the gas estimation client
	client := coregrpc.NewBlockAPIClient(cctx.GRPCClient)

	status, err := client.Status(context.Background(), &coregrpc.StatusRequest{})
	require.NoError(t, err)
	assert.Greater(t, status.SyncInfo.LatestBlockHeight, int64(1))
}

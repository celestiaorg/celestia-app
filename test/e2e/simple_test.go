package e2e

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/test/txsim"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/stretchr/testify/require"
)

const (
	latestVersion = "v1.0.0-rc9"
	seed          = 42 // the meaning of life
)

// This test runs a simple testnet with 4 validators. It submits both MsgPayForBlobs
// and MsgSends over 30 seconds and then asserts that at least 10 transactions were
// committed.
func TestE2ESimple(t *testing.T) {
	if os.Getenv("E2E") == "" {
		t.Skip("skipping e2e test")
	}

	testnet, err := New(t.Name(), seed)
	require.NoError(t, err)
	t.Cleanup(testnet.Cleanup)
	require.NoError(t, testnet.CreateGenesisNodes(4, latestVersion, 10000000))

	kr, err := testnet.CreateGenesisAccount("alice", 1e12)
	require.NoError(t, err)

	require.NoError(t, testnet.Setup())
	require.NoError(t, testnet.Start())

	sequences := txsim.NewBlobSequence(txsim.NewRange(200, 4000), txsim.NewRange(1, 3)).Clone(5)
	sequences = append(sequences, txsim.NewSendSequence(4, 1000, 100).Clone(5)...)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = txsim.Run(ctx, testnet.RPCEndpoints(), testnet.GRPCEndpoints(), kr, "", seed, 3*time.Second, sequences...)
	require.True(t, errors.Is(err, context.DeadlineExceeded), err.Error())

	blockchain, err := testnode.ReadBlockchain(context.Background(), testnet.Node(0).AddressRPC())
	require.NoError(t, err)

	totalTxs := 0
	for _, block := range blockchain {
		totalTxs += len(block.Data.Txs)
	}
	require.Greater(t, totalTxs, 10)
}

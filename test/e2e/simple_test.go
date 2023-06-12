package e2e

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/test/txsim"
	"github.com/stretchr/testify/require"
)

const (
	latestVersion = "latest"
	seed          = 42
)

func TestE2ESimple(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	testnet := New(seed)
	require.NoError(t, testnet.CreateGenesisNodes(4, latestVersion, 10000))

	kr, err := testnet.CreateGenesisAccount("alice", 10000000)
	require.NoError(t, err)

	require.NoError(t, testnet.Setup())
	require.NoError(t, testnet.Start())

	sequences := txsim.NewBlobSequence(txsim.NewRange(200, 4000), txsim.NewRange(1, 3)).Clone(5)
	sequences = append(sequences, txsim.NewSendSequence(4, 1000, 100).Clone(5)...)

	err = txsim.Run(ctx, testnet.RPCEndpoints(), testnet.GRPCEndpoints(), kr, seed, 1*time.Second, sequences...)
	require.True(t, errors.Is(err, context.DeadlineExceeded), err.Error())
}

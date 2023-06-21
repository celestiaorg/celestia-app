package e2e

import (
	"context"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/test/txsim"
	"github.com/celestiaorg/knuu/pkg/knuu"
	"github.com/stretchr/testify/require"
)

const (
	latestVersion = "v1.0.0-rc4"
	seed          = 42
)

func TestE2ESimple(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping e2e test in short mode")
	}
	identifier := fmt.Sprintf("%s_%s", t.Name(), time.Now().Format("20060102_150405"))
	err := knuu.InitializeWithIdentifier(identifier)
	testnet := New(seed)
	require.NoError(t, testnet.CreateGenesisNodes(4, latestVersion, 10000))

	kr, err := testnet.CreateGenesisAccount("alice", 10000000)
	require.NoError(t, err)

	require.NoError(t, testnet.Setup())
	require.NoError(t, testnet.Start())

	sequences := txsim.NewBlobSequence(txsim.NewRange(200, 4000), txsim.NewRange(1, 3)).Clone(5)
	sequences = append(sequences, txsim.NewSendSequence(4, 1000, 100).Clone(5)...)

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	err = txsim.Run(ctx, testnet.RPCEndpoints(), testnet.GRPCEndpoints(), kr, seed, 1*time.Second, sequences...)
	require.True(t, errors.Is(err, context.DeadlineExceeded), err.Error())
}

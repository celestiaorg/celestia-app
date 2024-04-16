package e2e

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/test/txsim"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/stretchr/testify/require"
)

const seed = 42

var latestVersion = "latest"

// This test runs a simple testnet with 4 validators. It submits both MsgPayForBlobs
// and MsgSends over 30 seconds and then asserts that at least 10 transactions were
// committed.
func TestE2ESimple(t *testing.T) {
	if os.Getenv("KNUU_NAMESPACE") != "test" {
		t.Skip("skipping e2e test")
	}

	if os.Getenv("E2E_LATEST_VERSION") != "" {
		latestVersion = os.Getenv("E2E_LATEST_VERSION")
		_, isSemVer := ParseVersion(latestVersion)
		switch {
		case isSemVer:
		case latestVersion == "latest":
		case len(latestVersion) == 7:
		case len(latestVersion) >= 8:
			// assume this is a git commit hash (we need to trim the last digit to match the docker image tag)
			latestVersion = latestVersion[:7]
		default:
			t.Fatalf("unrecognised version: %s", latestVersion)
		}
	}
	t.Log("Running simple e2e test", "version", latestVersion)

	testnet, err := New(t.Name(), seed, GetGrafanaInfoFromEnvVar(), false)
	require.NoError(t, err)
	t.Cleanup(testnet.Cleanup)

	t.Log("Creating testnet validators")
	require.NoError(t, testnet.CreateGenesisNodes(4, latestVersion, 10000000,
		0, defaultResources))

	t.Log("Creating account")
	kr, err := testnet.CreateAccount("alice", 1e12, "")
	require.NoError(t, err)

	t.Log("Setting up testnet")
	require.NoError(t, testnet.Setup())
	t.Log("Starting testnet")
	require.NoError(t, testnet.Start())

	t.Log("Running txsim")
	sequences := txsim.NewBlobSequence(txsim.NewRange(200, 4000), txsim.NewRange(1, 3)).Clone(5)
	sequences = append(sequences, txsim.NewSendSequence(4, 1000, 100).Clone(5)...)

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	opts := txsim.DefaultOptions().WithSeed(seed).SuppressLogs()
	err = txsim.Run(ctx, testnet.GRPCEndpoints()[0], kr, encCfg, opts, sequences...)
	require.True(t, errors.Is(err, context.DeadlineExceeded), err.Error())

	t.Log("Reading blockchain")
	blockchain, err := testnode.ReadBlockchain(context.Background(), testnet.Node(0).AddressRPC())
	require.NoError(t, err)

	totalTxs := 0
	for _, block := range blockchain {
		require.Equal(t, appconsts.LatestVersion, block.Version.App)
		totalTxs += len(block.Data.Txs)
	}
	require.Greater(t, totalTxs, 10)
}

package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/stretchr/testify/require"
)

func TestE2EThroughput(t *testing.T) {
	if os.Getenv("KNUU_NAMESPACE") != "test-sanaz" {
		t.Skip("skipping e2e throughput test")
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

	t.Log("Running throughput test", "version", latestVersion)

	// create a new testnet
	testnet, err := New(t.Name(), seed, GetGrafanaInfoFromEnvVar())
	require.NoError(t, err)
	t.Cleanup(testnet.Cleanup)

	// add 2 validators
	require.NoError(t, testnet.CreateGenesisNodes(2, latestVersion, 10000000,
		0, defaultResources))

	// obtain the GRPC endpoints of the validators
	gRPCEndpoints, err := testnet.RemoteGRPCEndpoints()
	require.NoError(t, err)
	t.Log("txsim GRPC endpoint", gRPCEndpoints)

	rPCEndPoints, err := testnet.RemoteRPCEndpoints()
	require.NoError(t, err)
	t.Log("RPC endpoint", rPCEndPoints)

	t.Log("Creating txsim nodes")
	// create txsim nodes and point them to the validators
	txsimVersion := "a954bc1"

	err = testnet.CreateAndSetupTxSimNodes(txsimVersion, seed, 1,
		"10000-10000", 3, defaultResources,
		gRPCEndpoints[:], rPCEndPoints[:])
	require.NoError(t, err)

	// start the testnet
	t.Log("Setting up testnet")
	require.NoError(t, testnet.Setup()) // configs, genesis files, etc.
	t.Log("Starting testnet")
	require.NoError(t, testnet.Start())

	// once the testnet is up, start the txsim
	t.Log("Starting txsim nodes")
	err = testnet.StartTxSimNodes()
	require.NoError(t, err)

	// wait some time for the txsim to submit transactions
	time.Sleep(1 * time.Minute)

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

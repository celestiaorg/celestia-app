package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	v2 "github.com/celestiaorg/celestia-app/pkg/appconsts/v2"
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
	testnet, err := New(t.Name(), seed)
	require.NoError(t, err)
	t.Cleanup(testnet.Cleanup)

	// add 4 validators
	require.NoError(t, testnet.CreateGenesisNodes(4, latestVersion, 10000000,
		0, Resources{"200Mi", "200Mi", "300m", "200Mi"}))

	// obtain the GRPC endpoints of the validators
	gRPCEndpoints, err := testnet.RemoteGRPCEndpoints()
	require.NoError(t, err)
	t.Log("txsim GRPC endpoint", gRPCEndpoints[0])

	// create a txsim node and points it to the first validator
	txsimGRPEndPoint := gRPCEndpoints[0]
	txsimVersion := "65c1a8e" // TODO pull the latest version of txsim if possible
	err = testnet.CreateAndSetupTxSimNode(
		"txsim",
		txsimVersion,
		seed,
		1,
		fmt.Sprintf("%d-%d", 50*1024, 100*1024),
		3,
		Resources{"200Mi", "200Mi", "300m", "1Gi"},
		txsimGRPEndPoint)
	require.NoError(t, err)

	// start the testnet
	require.NoError(t, testnet.Setup()) // configs, genesis files, etc.
	require.NoError(t, testnet.Start())

	// once the testnet is up, start the txsim
	t.Log("Starting txsim")
	err = testnet.StartTxSimNode()
	require.NoError(t, err)

	// wait some time for the txsim to submit transactions
	time.Sleep(30 * time.Second)

	blockchain, err := testnode.ReadBlockchain(context.Background(), testnet.Node(0).AddressRPC())
	require.NoError(t, err)

	totalTxs := 0
	for _, block := range blockchain {
		require.Equal(t, v2.Version, block.Version.App)
		totalTxs += len(block.Data.Txs)
	}
	require.Greater(t, totalTxs, 10)
}

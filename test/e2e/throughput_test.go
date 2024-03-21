package e2e

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	v2 "github.com/celestiaorg/celestia-app/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/stretchr/testify/require"
)

// const seed = 42
//
// var latestVersion = "latest"

func TestE2EThroughput(t *testing.T) {
	if os.Getenv("KNUU_NAMESPACE") != "test" {
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

	testnet, err := New(t.Name(), seed)
	require.NoError(t, err)
	t.Cleanup(testnet.Cleanup)
	require.NoError(t, testnet.CreateGenesisNodes(4, latestVersion, 10000000,
		0, Resources{"200Mi", "200Mi", "300m", "200Mi"}))

	// create an account, store it in a temp directory and add the account as genesis account
	txsimKeyringDir := filepath.Join(os.TempDir(), "txsim")
	_, err = testnet.CreateTxSimAccount("alice", 1e12, txsimKeyringDir)
	require.NoError(t, err)

	// start the testnet
	require.NoError(t, testnet.Setup()) // configs, genesis files, etc.
	require.NoError(t, testnet.Start())

	t.Log("Starting txsim")
	// TODO pull the latest version if possible
	// TODO increase blob size range
	IP, err := testnet.nodes[0].Instance.GetIP()
	require.NoError(t, err)
	endpoint := fmt.Sprintf("%s:9090", IP)
	t.Log("GRPC endpoint", endpoint)
	err = testnet.SetupTxsimNode("txsim", "65c1a8e",
		endpoint, seed, 1, fmt.Sprintf("%d-%d", 50*1024, 100*1024), 3,
		Resources{"200Mi", "200Mi", "300m", "1Gi"},
		txsimRootDir, txsimKeyringDir)
	require.NoError(t, err)

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

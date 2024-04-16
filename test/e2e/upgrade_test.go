package e2e

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/v2/test/txsim"
	"github.com/celestiaorg/knuu/pkg/knuu"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/rpc/client/http"
)

// This will only run tests within the v1 major release cycle
const MajorVersion = v1.Version

func TestMinorVersionCompatibility(t *testing.T) {
	// FIXME: This test currently panics in InitGenesis
	t.Skip("test not working")
	if os.Getenv("KNUU_NAMESPACE") != "test" {
		t.Skip("skipping e2e test")
	}

	if os.Getenv("E2E_VERSIONS") == "" {
		t.Skip("skipping e2e test: E2E_VERSIONS not set")
	}

	versionStr := os.Getenv("E2E_VERSIONS")
	versions := ParseVersions(versionStr).FilterMajor(MajorVersion).FilterOutReleaseCandidates()
	if len(versions) == 0 {
		t.Skip("skipping e2e test: no versions to test")
	}
	numNodes := 4
	r := rand.New(rand.NewSource(seed))
	t.Log("Running minor version compatibility test", "versions", versions)

	testnet, err := New(t.Name(), seed, GetGrafanaInfoFromEnvVar(), false)
	require.NoError(t, err)
	t.Cleanup(testnet.Cleanup)
	testnet.SetConsensusParams(app.DefaultInitialConsensusParams())

	// preload all docker images
	preloader, err := knuu.NewPreloader()
	require.NoError(t, err)
	t.Cleanup(func() { _ = preloader.EmptyImages() })
	for _, v := range versions {
		err := preloader.AddImage(DockerImageName(v.String()))
		require.NoError(t, err)
	}

	for i := 0; i < numNodes; i++ {
		// each node begins with a random version within the same major version set
		v := versions.Random(r).String()
		t.Log("Starting node", "node", i, "version", v)
		require.NoError(t, testnet.CreateGenesisNode(v, 10000000, 0, defaultResources))
	}

	kr, err := testnet.CreateAccount("alice", 1e12, "")
	require.NoError(t, err)

	require.NoError(t, testnet.Setup())
	require.NoError(t, testnet.Start())

	// TODO: with upgrade tests we should simulate a far broader range of transactions
	sequences := txsim.NewBlobSequence(txsim.NewRange(200, 4000), txsim.NewRange(1, 3)).Clone(5)
	sequences = append(sequences, txsim.NewSendSequence(4, 1000, 100).Clone(5)...)

	errCh := make(chan error)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	opts := txsim.DefaultOptions().WithSeed(seed).SuppressLogs()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		errCh <- txsim.Run(ctx, testnet.GRPCEndpoints()[0], kr, encCfg, opts, sequences...)
	}()

	for i := 0; i < len(versions)*2; i++ {
		// FIXME: skip the first node because we need them available to
		// submit txs
		if i%numNodes == 0 {
			continue
		}
		client, err := testnet.Node(i % numNodes).Client()
		require.NoError(t, err)
		heightBefore, err := getHeight(ctx, client, time.Second)
		require.NoError(t, err)
		newVersion := versions.Random(r).String()
		t.Log("Upgrading node", "node", i%numNodes, "version", newVersion)
		err = testnet.Node(i % numNodes).Upgrade(newVersion)
		require.NoError(t, err)
		// wait for the node to reach two more heights
		err = waitForHeight(ctx, client, heightBefore+2, 30*time.Second)
		require.NoError(t, err)
	}

	heights := make([]int64, 4)
	for i := 0; i < numNodes; i++ {
		client, err := testnet.Node(i).Client()
		require.NoError(t, err)
		heights[i], err = getHeight(ctx, client, time.Second)
		require.NoError(t, err)
	}

	t.Log("checking that all nodes are at the same height")
	const maxPermissableDiff = 2
	for i := 0; i < len(heights); i++ {
		for j := i + 1; j < len(heights); j++ {
			diff := heights[i] - heights[j]
			if diff > maxPermissableDiff {
				t.Fatalf("node %d is behind node %d by %d blocks", j, i, diff)
			}
		}
	}

	// end the tx sim
	cancel()

	err = <-errCh
	require.True(t, errors.Is(err, context.Canceled), err.Error())
}

func TestMajorUpgradeToV2(t *testing.T) {
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
		case len(latestVersion) == 8:
			// assume this is a git commit hash (we need to trim the last digit to match the docker image tag)
			latestVersion = latestVersion[:7]
		default:
			t.Fatalf("unrecognised version: %s", latestVersion)
		}
	}

	numNodes := 4
	upgradeHeight := int64(12)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	testnet, err := New(t.Name(), seed, GetGrafanaInfoFromEnvVar(), false)
	require.NoError(t, err)
	t.Cleanup(testnet.Cleanup)

	preloader, err := knuu.NewPreloader()
	require.NoError(t, err)
	t.Cleanup(func() { _ = preloader.EmptyImages() })
	err = preloader.AddImage(DockerImageName(latestVersion))
	require.NoError(t, err)

	for i := 0; i < numNodes; i++ {
		require.NoError(t, testnet.CreateGenesisNode(latestVersion, 10000000,
			upgradeHeight, defaultResources))
	}

	kr, err := testnet.CreateAccount("alice", 1e12, "")
	require.NoError(t, err)

	require.NoError(t, testnet.Setup())
	require.NoError(t, testnet.Start())

	errCh := make(chan error)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	opts := txsim.DefaultOptions().WithSeed(seed).SuppressLogs()
	sequences := txsim.NewBlobSequence(txsim.NewRange(200, 4000), txsim.NewRange(1, 3)).Clone(5)
	sequences = append(sequences, txsim.NewSendSequence(4, 1000, 100).Clone(5)...)
	go func() {
		errCh <- txsim.Run(ctx, testnet.GRPCEndpoints()[0], kr, encCfg, opts, sequences...)
	}()

	// assert that the network is initially running on v1
	heightBefore := upgradeHeight - 1
	for i := 0; i < numNodes; i++ {
		client, err := testnet.Node(i).Client()
		require.NoError(t, err)
		require.NoError(t, waitForHeight(ctx, client, upgradeHeight, time.Minute))
		resp, err := client.Header(ctx, &heightBefore)
		require.NoError(t, err)
		require.Equal(t, v1.Version, resp.Header.Version.App, "version mismatch before upgrade")
		resp, err = client.Header(ctx, &upgradeHeight)
		require.NoError(t, err)
		require.Equal(t, v2.Version, resp.Header.Version.App, "version mismatch after upgrade")
	}

	// end txsim
	cancel()

	err = <-errCh
	require.True(t, strings.Contains(err.Error(), context.Canceled.Error()), err.Error())
}

func getHeight(ctx context.Context, client *http.HTTP, period time.Duration) (int64, error) {
	timer := time.NewTimer(period)
	ticker := time.NewTicker(100 * time.Millisecond)
	for {
		select {
		case <-timer.C:
			return 0, fmt.Errorf("failed to get height after %.2f seconds", period.Seconds())
		case <-ticker.C:
			status, err := client.Status(ctx)
			if err == nil {
				return status.SyncInfo.LatestBlockHeight, nil
			}
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return 0, err
			}
		}
	}
}

func waitForHeight(ctx context.Context, client *http.HTTP, height int64, period time.Duration) error {
	timer := time.NewTimer(period)
	ticker := time.NewTicker(100 * time.Millisecond)
	for {
		select {
		case <-timer.C:
			return fmt.Errorf("failed to reach height %d in %.2f seconds", height, period.Seconds())
		case <-ticker.C:
			status, err := client.Status(ctx)
			if err != nil {
				return err
			}
			if status.SyncInfo.LatestBlockHeight >= height {
				return nil
			}
		}
	}
}

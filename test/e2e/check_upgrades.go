package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os/exec"
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/v2/test/e2e/testnets"
	"github.com/celestiaorg/celestia-app/v2/test/txsim"
	"github.com/celestiaorg/knuu/pkg/knuu"
	"github.com/tendermint/tendermint/rpc/client/http"
)

func MinorVersionCompatibility(logger *log.Logger) error {
	versionStr, err := getAllVersions()
	testnets.NoError("failed to get versions", err)
	versions := testnets.ParseVersions(versionStr).FilterMajor(MajorVersion).FilterOutReleaseCandidates()

	if len(versions) == 0 {
		logger.Fatal("no versions to test")
	}
	numNodes := 4
	r := rand.New(rand.NewSource(seed))
	logger.Println("Running minor version compatibility test", "versions", versions)

	testnet, err := testnets.New("runMinorVersionCompatibility", seed, nil, testnets.GetSampleTestManifest())
	testnets.NoError("failed to create testnet", err)

	defer testnet.Cleanup()

	testnet.SetConsensusParams(app.DefaultInitialConsensusParams())

	// preload all docker images
	preloader, err := knuu.NewPreloader()
	testnets.NoError("failed to create preloader", err)

	defer func() { _ = preloader.EmptyImages() }()
	for _, v := range versions {
		testnets.NoError("failed to add image", preloader.AddImage(testnets.DockerImageName(v.String())))
	}

	for i := 0; i < numNodes; i++ {
		// each node begins with a random version within the same major version set
		v := versions.Random(r).String()
		logger.Println("Starting node", "node", i, "version", v)
		testnets.NoError("failed to create genesis node", testnet.CreateGenesisNode(v, 10000000, 0, testnets.DefaultResources))
	}

	kr, err := testnet.CreateAccount("alice", 1e12, "")
	testnets.NoError("failed to create account", err)

	// start the testnet
	logger.Println("Setting up testnet")
	testnets.NoError("Failed to setup testnet", testnet.Setup())
	logger.Println("Starting testnet")
	testnets.NoError("Failed to start testnet", testnet.Start())

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
		testnets.NoError("failed to get client", err)

		heightBefore, err := getHeight(ctx, client, time.Second)
		testnets.NoError("failed to get height", err)

		newVersion := versions.Random(r).String()
		logger.Println("Upgrading node", "node", i%numNodes+1, "version", newVersion)
		testnets.NoError("failed to upgrade node", testnet.Node(i%numNodes+1).Upgrade(newVersion))
		// wait for the node to reach two more heights
		testnets.NoError("failed to wait for height", waitForHeight(ctx, client, heightBefore+2, 30*time.Second))
	}

	heights := make([]int64, 4)
	for i := 0; i < numNodes; i++ {
		client, err := testnet.Node(i).Client()
		testnets.NoError("failed to get client", err)
		heights[i], err = getHeight(ctx, client, time.Second)
		testnets.NoError("failed to get height", err)
	}

	logger.Println("checking that all nodes are at the same height")
	const maxPermissableDiff = 2
	for i := 0; i < len(heights); i++ {
		for j := i + 1; j < len(heights); j++ {
			diff := heights[i] - heights[j]
			if diff > maxPermissableDiff {
				logger.Fatalf("node %d is behind node %d by %d blocks", j, i, diff)
			}
		}
	}

	// end the tx sim
	cancel()

	err = <-errCh

	if !errors.Is(err, context.Canceled) {
		return fmt.Errorf("expected context.Canceled error, got: %w", err)
	}
	return nil
}

func MajorUpgradeToV2(logger *log.Logger) error {
	latestVersion, err := testnets.GetLatestVersion()
	testnets.NoError("failed to get latest version", err)

	logger.Println("Running major upgrade to v2 test", "version", latestVersion)

	numNodes := 4
	upgradeHeight := int64(12)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	logger.Println("Creating testnet")
	manifest := testnets.GetSampleTestManifest()
	manifest.CelestiaAppVersion = latestVersion
	testnet, err := testnets.New("runMajorUpgradeToV2", seed, nil, manifest)
	testnets.NoError("failed to create testnet", err)

	defer testnet.Cleanup()

	preloader, err := knuu.NewPreloader()
	testnets.NoError("failed to create preloader", err)

	defer func() { _ = preloader.EmptyImages() }()
	testnets.NoError("failed to add image", preloader.AddImage(testnets.DockerImageName(latestVersion)))

	logger.Println("Creating genesis nodes")
	for i := 0; i < numNodes; i++ {
		err := testnet.CreateGenesisNode(latestVersion, 10000000, upgradeHeight, testnets.DefaultResources)
		testnets.NoError("failed to create genesis node", err)
	}

	kr, err := testnet.CreateAccount("alice", 1e12, "")
	testnets.NoError("failed to create account", err)
	// start the testnet

	logger.Println("Setting up testnet")
	testnets.NoError("Failed to setup testnet", testnet.Setup())
	logger.Println("Starting testnet")
	testnets.NoError("Failed to start testnet", testnet.Start())

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
		testnets.NoError("failed to get client", err)

		testnets.NoError("failed to wait for height", waitForHeight(ctx, client, upgradeHeight, time.Minute))

		resp, err := client.Header(ctx, &heightBefore)
		testnets.NoError("failed to get header", err)
		logger.Println("Node", i, "is running on version", resp.Header.Version.App)
		if resp.Header.Version.App != v1.Version {
			return fmt.Errorf("version mismatch before upgrade: expected %d, got %d", v1.Version, resp.Header.Version.App)
		}

		resp, err = client.Header(ctx, &upgradeHeight)
		testnets.NoError("failed to get header", err)
		if resp.Header.Version.App != v2.Version {
			return fmt.Errorf("version mismatch before upgrade: expected %d, got %d", v2.Version, resp.Header.Version.App)
		}
	}

	// end txsim
	cancel()

	err = <-errCh
	if !strings.Contains(err.Error(), context.Canceled.Error()) {
		return fmt.Errorf("expected context.Canceled error, got: %w", err)
	}
	return nil
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

func getAllVersions() (string, error) {
	cmd := exec.Command("git", "tag", "-l")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get git tags: %v", err)
	}
	allVersions := strings.Split(strings.TrimSpace(string(output)), "\n")
	return strings.Join(allVersions, " "), nil
}

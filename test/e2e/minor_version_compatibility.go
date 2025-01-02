//nolint:staticcheck
package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	v1 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v2"
	v3 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v3"
	"github.com/celestiaorg/celestia-app/v3/test/e2e/testnet"
	"github.com/celestiaorg/knuu/pkg/knuu"
)

func MinorVersionCompatibility(logger *log.Logger) error {
	const (
		testName = "MinorVersionCompatibility"
		numNodes = 4
		// retractedCelestiaApp is a version of celestia-app that was retracted.
		retractedCelestiaApp = "v1.8.0"
	)

	versionStr, err := getAllVersions()
	testnet.NoError("failed to get versions", err)

	retracted, ok := testnet.ParseVersion(retractedCelestiaApp)
	if !ok {
		logger.Fatal("failed to parse retracted version")
	}

	versions1 := testnet.ParseVersions(versionStr).FilterMajor(v1.Version).FilterOutReleaseCandidates().FilterOut(retracted)
	versions2 := testnet.ParseVersions(versionStr).FilterMajor(v2.Version).FilterOutReleaseCandidates()
	versions3 := testnet.ParseVersions(versionStr).FilterMajor(v3.Version).FilterOutReleaseCandidates()
	versions := slices.Concat(versions1, versions2, versions3)

	if len(versions) == 0 {
		logger.Fatal("no versions to test")
	}
	seed := testnet.DefaultSeed
	r := rand.New(rand.NewSource(seed))
	logger.Printf("Running %s test with versions %s", testName, versions)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	identifier := fmt.Sprintf("%s_%s", testName, time.Now().Format(timeFormat))
	kn, err := knuu.New(ctx, knuu.Options{
		Scope:        identifier,
		ProxyEnabled: true,
	})
	testnet.NoError("failed to initialize Knuu", err)

	kn.HandleStopSignal(ctx)
	logger.Printf("Knuu initialized with scope %s", kn.Scope)

	testNet, err := testnet.New(logger, kn, testnet.Options{Seed: seed})
	testnet.NoError("failed to create testnet", err)

	defer testNet.Cleanup(ctx)

	testNet.SetConsensusParams(app.DefaultInitialConsensusParams())

	// preload all docker images
	preloader, err := testNet.NewPreloader()
	testnet.NoError("failed to create preloader", err)

	defer func() { _ = preloader.EmptyImages(ctx) }()
	for _, v := range versions {
		testnet.NoError("failed to add image", preloader.AddImage(ctx, testnet.DockerImageName(v.String())))
	}

	for i := 0; i < numNodes; i++ {
		// each node begins with a random version within the same major version set
		v := versions.Random(r).String()
		logger.Println("Starting node", "node", i, "version", v)

		testnet.NoError("failed to create genesis node",
			testNet.CreateGenesisNode(ctx, v, 10000000, 0, testnet.DefaultResources, false))
	}

	logger.Println("Creating txsim")
	endpoints, err := testNet.RemoteGRPCEndpoints()
	testnet.NoError("failed to get remote gRPC endpoints", err)
	upgradeSchedule := map[int64]uint64{}
	err = testNet.CreateTxClient(ctx, "txsim", testnet.TxsimVersion, 1, "100-2000", 100, testnet.DefaultResources, endpoints[0], upgradeSchedule)
	testnet.NoError("failed to create tx client", err)

	// start the testnet
	logger.Println("Setting up testnet")
	testnet.NoError("Failed to setup testnet", testNet.Setup(ctx))
	logger.Println("Starting testnet")
	testnet.NoError("Failed to start testnet", testNet.Start(ctx))

	for i := 0; i < len(versions)*2; i++ {
		// FIXME: skip the first node because we need them available to
		// submit txs
		if i%numNodes == 0 {
			continue
		}
		client, err := testNet.Node(i % numNodes).Client()
		testnet.NoError("failed to get client", err)

		heightBefore, err := getHeight(ctx, client, time.Second)
		testnet.NoError("failed to get height", err)

		newVersion := versions.Random(r).String()
		logger.Println("Upgrading node", "node", i%numNodes+1, "version", newVersion)
		testnet.NoError("failed to upgrade node", testNet.Node(i%numNodes).Upgrade(ctx, newVersion))
		time.Sleep(10 * time.Second)
		// wait for the node to reach two more heights
		testnet.NoError("failed to wait for height", waitForHeight(ctx, client, heightBefore+2, time.Minute))
	}

	heights := make([]int64, 4)
	for i := 0; i < numNodes; i++ {
		client, err := testNet.Node(i).Client()
		testnet.NoError("failed to get client", err)
		heights[i], err = getHeight(ctx, client, time.Second)
		testnet.NoError("failed to get height", err)
	}

	logger.Println("checking that all nodes are at the same height")
	const maxPermissibleDiff = 2
	for i := 0; i < len(heights); i++ {
		for j := i + 1; j < len(heights); j++ {
			diff := heights[i] - heights[j]
			if diff > maxPermissibleDiff {
				logger.Fatalf("node %d is behind node %d by %d blocks", j, i, diff)
			}
		}
	}

	return nil
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

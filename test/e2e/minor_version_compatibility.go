package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"os/exec"
	"slices"
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/v2/test/e2e/testnet"
	"github.com/celestiaorg/celestia-app/v2/test/txsim"
	"github.com/celestiaorg/knuu/pkg/knuu"
)

func MinorVersionCompatibility(logger *log.Logger) error {
	versionStr, err := getAllVersions()
	testnet.NoError("failed to get versions", err)
	versions1 := testnet.ParseVersions(versionStr).FilterMajor(v1.Version).FilterOutReleaseCandidates()
	versions2 := testnet.ParseVersions(versionStr).FilterMajor(v2.Version) // include release candidates for v2 because there isn't an official release yet.
	versions := slices.Concat(versions1, versions2)

	if len(versions) == 0 {
		logger.Fatal("no versions to test")
	}
	numNodes := 4
	r := rand.New(rand.NewSource(seed))
	logger.Println("Running minor version compatibility test", "versions", versions)

	testNet, err := testnet.New("runMinorVersionCompatibility", seed, nil, "test")
	testnet.NoError("failed to create testnet", err)

	defer testNet.Cleanup()

	testNet.SetConsensusParams(app.DefaultInitialConsensusParams())

	// preload all docker images
	preloader, err := knuu.NewPreloader()
	testnet.NoError("failed to create preloader", err)

	defer func() { _ = preloader.EmptyImages() }()
	for _, v := range versions {
		testnet.NoError("failed to add image", preloader.AddImage(testnet.DockerImageName(v.String())))
	}

	for i := 0; i < numNodes; i++ {
		// each node begins with a random version within the same major version set
		v := versions.Random(r).String()
		logger.Println("Starting node", "node", i, "version", v)
		testnet.NoError("failed to create genesis node", testNet.CreateGenesisNode(v, 10000000, 0, testnet.DefaultResources))
	}

	kr, err := testNet.CreateAccount("alice", 1e12, "")
	testnet.NoError("failed to create account", err)

	// start the testnet
	logger.Println("Setting up testnet")
	testnet.NoError("Failed to setup testnet", testNet.Setup())
	logger.Println("Starting testnet")
	testnet.NoError("Failed to start testnet", testNet.Start())

	// TODO: with upgrade tests we should simulate a far broader range of transactions
	sequences := txsim.NewBlobSequence(txsim.NewRange(200, 4000), txsim.NewRange(1, 3)).Clone(5)
	sequences = append(sequences, txsim.NewSendSequence(4, 1000, 100).Clone(5)...)

	errCh := make(chan error)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	opts := txsim.DefaultOptions().WithSeed(seed).SuppressLogs()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go func() {
		errCh <- txsim.Run(ctx, testNet.GRPCEndpoints()[0], kr, encCfg, opts, sequences...)
	}()

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
		testnet.NoError("failed to upgrade node", testNet.Node(i%numNodes).Upgrade(newVersion))
		time.Sleep(10 * time.Second)
		// wait for the node to reach two more heights
		testnet.NoError("failed to wait for height", waitForHeight(ctx, client, heightBefore+2, 30*time.Second))
	}

	heights := make([]int64, 4)
	for i := 0; i < numNodes; i++ {
		client, err := testNet.Node(i).Client()
		testnet.NoError("failed to get client", err)
		heights[i], err = getHeight(ctx, client, time.Second)
		testnet.NoError("failed to get height", err)
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

func getAllVersions() (string, error) {
	cmd := exec.Command("git", "tag", "-l")
	output, err := cmd.Output()
	if err != nil {
		return "", fmt.Errorf("failed to get git tags: %v", err)
	}
	allVersions := strings.Split(strings.TrimSpace(string(output)), "\n")
	return strings.Join(allVersions, " "), nil
}

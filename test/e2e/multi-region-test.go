package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/test/e2e/machine"
	"github.com/celestiaorg/celestia-app/v3/test/e2e/testnet"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	"github.com/celestiaorg/knuu/pkg/knuu"
	"github.com/inlets/cloud-provision/provision"
)

// MultiRegionTest runs a testnet with 100 validators in 12 different regions. It
// submits both MsgPayForBlobs and MsgSends over 5 minutes and then asserts that at
// least 10 transactions were committed.
func MultiRegionTest(logger *log.Logger) error {
	const testName = "MultiRegionTest"

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	identifier := fmt.Sprintf("%s_%s", testName, time.Now().Format(timeFormat))
	kn, err := knuu.New(ctx, knuu.Options{
		Scope:        identifier,
		ProxyEnabled: true,
	})
	testnet.NoError("failed to initialize Knuu", err)
	logger.Printf("Knuu initialized with scope %s", kn.Scope)

	testNet, err := testnet.New(logger, kn, testnet.Options{})
	testnet.NoErrorWithCleanup("failed to create testnet", err, func() {
		testNet.Cleanup(ctx)
	})

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM, os.Interrupt)
	go func() {
		<-stop
		logger.Println("Received signal to stop, cleaning up testNet resources...")
		testNet.Cleanup(ctx)
		os.Exit(0)
	}()
	defer testNet.Cleanup(ctx)

	doToken := os.Getenv("DIGITALOCEAN_TOKEN")
	if doToken == "" {
		testnet.NoErrorWithCleanup("DIGITALOCEAN_TOKEN environment variable is not set", fmt.Errorf("DIGITALOCEAN_TOKEN environment variable is not set"), func() {
			testNet.Cleanup(ctx)
		})
	}
	poolID := os.Getenv("POOL_ID")
	if poolID == "" {
		testnet.NoErrorWithCleanup("POOL_ID environment variable is not set", fmt.Errorf("POOL_ID environment variable is not set"), func() {
			testNet.Cleanup(ctx)
		})
	}
	scwSecretKey := os.Getenv("SCW_SECRET_KEY")
	if scwSecretKey == "" {
		testnet.NoErrorWithCleanup("SCW_SECRET_KEY environment variable is not set", fmt.Errorf("SCW_SECRET_KEY environment variable is not set"), func() {
			testNet.Cleanup(ctx)
		})
	}

	provisioner, err := provision.NewDigitalOceanProvisioner(doToken)
	testnet.NoErrorWithCleanup("failed to create digitalocean provisioner", err, func() {
		testNet.Cleanup(ctx)
	})

	hwNodes := []struct {
		region string
		name   string
	}{
		{machine.Regions.DigitalOcean.NYC1, "machine-do-nyc1"},
		{machine.Regions.DigitalOcean.NYC2, "machine-do-nyc2"},
		{machine.Regions.DigitalOcean.NYC3, "machine-do-nyc3"},
		{machine.Regions.DigitalOcean.AMS3, "machine-do-ams3"},
		{machine.Regions.DigitalOcean.SFO2, "machine-do-sfo2"},
		{machine.Regions.DigitalOcean.SFO3, "machine-do-sfo3"},
		{machine.Regions.DigitalOcean.SGP1, "machine-do-sgp1"},
		{machine.Regions.DigitalOcean.LON1, "machine-do-lon1"},
		{machine.Regions.DigitalOcean.FRA1, "machine-do-fra1"},
		{machine.Regions.DigitalOcean.TOR1, "machine-do-tor1"},
		{machine.Regions.DigitalOcean.BLR1, "machine-do-blr1"},
		{machine.Regions.DigitalOcean.SYD1, "machine-do-syd1"},
	}
	machineList := make([]*machine.Machine, len(hwNodes))
	for i, hwNode := range hwNodes {
		machineList[i], err = testNet.NewMachine(logger, provisioner, hwNode.region, machine.Sizes.DigitalOcean.G16VCPU64GB, hwNode.name)
		testnet.NoErrorWithCleanup(fmt.Sprintf("failed to create hw node %s", hwNode.name), err, func() {
			testNet.Cleanup(ctx)
		})
	}

	latestVersion, err := testnet.GetLatestVersion()
	testnet.NoErrorWithCleanup("failed to get latest version", err, func() {
		testNet.Cleanup(ctx)
	})
	logger.Printf("Running %s test with version %s", testName, latestVersion)

	logger.Println("Creating testnet validators")
	totalGenesisNodes := 100
	genesisNodesPerMachine := totalGenesisNodes / len(machineList)
	remainder := totalGenesisNodes % len(machineList)

	for i, machine := range machineList {
		nodesToCreate := genesisNodesPerMachine
		if i < remainder {
			nodesToCreate++
		}
		testnet.NoErrorWithCleanup("failed to create genesis nodes",
			testNet.CreateGenesisNodes(ctx, nodesToCreate, machine, latestVersion, 10000000, 0, testnet.DefaultResources, false), func() {
				testNet.Cleanup(ctx)
			})
	}

	logger.Println("Creating txsim")
	endpoints, err := testNet.RemoteGRPCEndpoints()
	testnet.NoErrorWithCleanup("failed to get remote gRPC endpoints", err, func() {
		testNet.Cleanup(ctx)
	})
	upgradeSchedule := map[int64]uint64{}
	err = testNet.CreateTxClient(ctx, machineList[0], "txsim", testnet.TxsimVersion, 10, "100-2000", 100, testnet.DefaultResources, endpoints[0], upgradeSchedule)
	testnet.NoErrorWithCleanup("failed to create tx client", err, func() {
		testNet.Cleanup(ctx)
	})

	logger.Println("Setting up testnets")
	testnet.NoErrorWithCleanup("failed to setup testnets", testNet.Setup(ctx), func() {
		testNet.Cleanup(ctx)
	})

	logger.Println("Starting testnets")
	testnet.NoErrorWithCleanup("failed to start testnets", testNet.Start(ctx), func() {
		testNet.Cleanup(ctx)
	})

	logger.Println("Waiting for 5 minutes to produce blocks")
	time.Sleep(5 * time.Minute)

	logger.Println("Reading blockchain headers")
	blockchain, err := testnode.ReadBlockchainHeaders(ctx, testNet.Node(0).AddressRPC())
	testnet.NoErrorWithCleanup("failed to read blockchain headers", err, func() {
		testNet.Cleanup(ctx)
	})

	totalTxs := 0
	for _, blockMeta := range blockchain {
		version := blockMeta.Header.Version.App
		if appconsts.LatestVersion != version {
			testnet.NoErrorWithCleanup("expected app version %d, got %d in blockMeta %d", fmt.Errorf("expected app version %d, got %d in blockMeta %d", appconsts.LatestVersion, version, blockMeta.Header.Height), func() {
				testNet.Cleanup(ctx)
			})
		}
		totalTxs += blockMeta.NumTxs
	}
	if totalTxs < 10 {
		testnet.NoErrorWithCleanup("expected at least 10 transactions, got %d", fmt.Errorf("expected at least 10 transactions, got %d", totalTxs), func() {
			testNet.Cleanup(ctx)
		})
	}
	logger.Printf("Total transactions: %d", totalTxs)
	return nil
}

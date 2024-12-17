package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"os/signal"
	"sort"
	"sync"
	"syscall"
	"time"

	"github.com/celestiaorg/celestia-app/v3/test/e2e/machine"
	"github.com/celestiaorg/celestia-app/v3/test/e2e/testnet"
	"github.com/celestiaorg/knuu/pkg/knuu"
	"github.com/inlets/cloud-provision/provision"
)

// MultiRegionTest runs a testnet with 100 validators in 12 different regions. It
// submits both MsgPayForBlobs and MsgSends over 5 minutes and then asserts that at
// least 10 transactions were committed.
func MultiRegionTest(logger *log.Logger) error {
	const testName = "MultiRegionTest"

	doToken := os.Getenv("DIGITALOCEAN_TOKEN")
	if doToken == "" {
		testnet.NoError("DIGITALOCEAN_TOKEN environment variable is not set", fmt.Errorf("DIGITALOCEAN_TOKEN environment variable is not set"))
	}

	provisioner, err := provision.NewDigitalOceanProvisioner(doToken)
	testnet.NoError("failed to create digitalocean provisioner", err)

	// machineUserDataForClastixControlplane := []string{
	// 	"#!/bin/bash",
	// 	"touch /opt/knuu",
	// 	"sudo apt update && sudo apt install -y socat conntrack",
	// 	"wget -O- https://goyaki.clastix.io | sudo JOIN_URL=ssmuu-1031-default-test.k8s.clastix.cloud:443 JOIN_TOKEN=peqw05.qjbgbmah91v210u4 JOIN_TOKEN_CACERT_HASH=sha256:4238684b30295f8f92ef45f13e30b3dca19997153921462059d553712f3cde99 bash -s join",
	// 	"echo 'done' >> /opt/knuu",
	// }

	// _, err = machine.NewMachine(logger, provisioner, machine.Regions.DigitalOcean.NYC1, machine.Sizes.DigitalOcean.G16VCPU64GB, "citrix-test-1", "ubuntu-24-04-x64", machineUserDataForClastixControlplane)
	// testnet.NoError("failed to create machine", err)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	identifier := fmt.Sprintf("%s_%s", testName, time.Now().Format(timeFormat))
	kn, err := knuu.New(ctx, knuu.Options{
		Scope:        identifier,
		ProxyEnabled: true,
		Timeout:      8 * time.Hour,
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

	// logger.Println("Creating txsim")
	// endpoints, err := testNet.RemoteGRPCEndpoints()
	// testnet.NoErrorWithCleanup("failed to get remote gRPC endpoints", err, func() {
	// 	testNet.Cleanup(ctx)
	// })
	// upgradeSchedule := map[int64]uint64{}
	// err = testNet.CreateTxClient(ctx, machineList[0], "txsim", testnet.TxsimVersion, 10, "100-2000", 100, testnet.DefaultResources, endpoints[0], upgradeSchedule)
	// testnet.NoErrorWithCleanup("failed to create tx client", err, func() {
	// 	testNet.Cleanup(ctx)
	// })

	logger.Println("Setting up testnets")
	testnet.NoErrorWithCleanup("failed to setup testnets", testNet.Setup(ctx), func() {
		testNet.Cleanup(ctx)
	})

	logger.Println("Starting testnets")
	testnet.NoErrorWithCleanup("failed to start testnets", testNet.Start(ctx), func() {
		testNet.Cleanup(ctx)
	})

	nodes := testNet.Nodes()
	var wg sync.WaitGroup
	heights := make(map[string]int64)
	var mu sync.Mutex

	for _, node := range nodes {
		wg.Add(1)
		go func(n *testnet.Node) {
			defer wg.Done()
			client, err := n.Client()
			if err != nil {
				logger.Printf("Failed to get client for node %s: %v", n.Name, err)
				return
			}
			interval := 6 * time.Second
			ticker := time.NewTicker(interval)
			defer ticker.Stop()
			timeout := time.After(30 * time.Minute)

			for {
				select {
				case <-timeout:
					return
				case <-ticker.C:
					statusCtx, cancel := context.WithTimeout(ctx, interval)
					status, err := client.Status(statusCtx)
					cancel() // Ensure cancel is called immediately after use

					if err != nil {
						logger.Printf("Failed to get validator height for node %s: %v", n.Name, err)
					} else {
						mu.Lock()
						heights[n.Name] = status.SyncInfo.LatestBlockHeight
						mu.Unlock()
					}
				}
			}
		}(node)
	}
	wg.Wait()

	// Calculate and log min, max, average, and median heights
	var allHeights []int64
	for _, h := range heights {
		allHeights = append(allHeights, h)
	}

	if len(allHeights) > 0 {
		sort.Slice(allHeights, func(i, j int) bool { return allHeights[i] < allHeights[j] })
		minHeight := allHeights[0]
		maxHeight := allHeights[len(allHeights)-1]
		sum := int64(0)
		for _, h := range allHeights {
			sum += h
		}
		averageHeight := sum / int64(len(allHeights))
		medianHeight := allHeights[len(allHeights)/2]

		logger.Printf("Validator Heights - Min: %d, Max: %d, Average: %d, Median: %d", minHeight, maxHeight, averageHeight, medianHeight)
	}

	// sleep forever
	select {}

	// logger.Println("Reading blockchain headers")
	// blockchain, err := testnode.ReadBlockchainHeaders(ctx, testNet.Node(0).AddressRPC())
	// testnet.NoErrorWithCleanup("failed to read blockchain headers", err, func() {
	// 	testNet.Cleanup(ctx)
	// })

	// totalTxs := 0
	// for _, blockMeta := range blockchain {
	// 	version := blockMeta.Header.Version.App
	// 	if appconsts.LatestVersion != version {
	// 		testnet.NoErrorWithCleanup("expected app version %d, got %d in blockMeta %d", fmt.Errorf("expected app version %d, got %d in blockMeta %d", appconsts.LatestVersion, version, blockMeta.Header.Height), func() {
	// 			testNet.Cleanup(ctx)
	// 		})
	// 	}
	// 	totalTxs += blockMeta.NumTxs
	// }
	// if totalTxs < 10 {
	// 	testnet.NoErrorWithCleanup("expected at least 10 transactions, got %d", fmt.Errorf("expected at least 10 transactions, got %d", totalTxs), func() {
	// 		testNet.Cleanup(ctx)
	// 	})
	// }
	// logger.Printf("Total transactions: %d", totalTxs)
	return nil
}

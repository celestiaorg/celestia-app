package e2e

import (
	"context"
	"fmt"
	"os"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	v1 "github.com/celestiaorg/celestia-app/pkg/appconsts/v1"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/plotutil"
	"gonum.org/v1/plot/vg"

	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/types"
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
	//maxResources := Resources{
	//	memoryRequest: "10Gi",
	//	memoryLimit:   "12Gi",
	//	cpu:           "6",
	//	volume:        "1Gi",
	//}
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
	txsimVersion := "a954bc1" // old: "cee9cd4" // "65c1a8e" // TODO pull the
	// latest version of txsim if possible

	err = testnet.CreateAndSetupTxSimNodes(txsimVersion, seed, 10,
		"99000-99000", 3, Resources{
			memoryRequest: "1Gi",
			memoryLimit:   "1Gi",
			cpu:           "2",
			volume:        "1Gi",
		},
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

	blockTimes, blockSizes, thputs, blockTimesNano := throughput(blockchain)
	t.Log("blockTimes", blockTimes)
	t.Log("blockTimesNano", blockTimesNano)
	t.Log("blockSizes", blockSizes)
	t.Log("thputs", thputs)
	plotData(blockSizes, fmt.Sprintf("blocksizes-%d.png", appconsts.DefaultGovMaxSquareSize),
		"Block Size", "Height",
		"Block Size")
	plotData(blockTimes, fmt.Sprintf("blocktimes-%d.png",
		appconsts.DefaultGovMaxSquareSize), "Block Time in seconds", "Height",
		"Block Time in seconds")
	plotData(thputs, fmt.Sprintf("throughputs-%d.png",
		appconsts.DefaultGovMaxSquareSize), "Throughput",
		"Height", "Throughput")

	totalTxs := 0
	for _, block := range blockchain {
		require.Equal(t, v1.Version, block.Version.App)
		totalTxs += len(block.Data.Txs)
	}
	require.Greater(t, totalTxs, 10)
}

func throughput(blockchain []*types.Block) ([]float64, []float64, []float64,
	[]float64) {
	blockTimes := make([]float64, 0, len(blockchain)-1)
	blockTimesNano := make([]float64, 0, len(blockchain)-1)
	blockSizes := make([]float64, 0, len(blockchain)-1)
	throughputs := make([]float64, 0, len(blockchain)-1)
	// timestamp of the last processed block
	lastBlockTS := blockchain[0].Header.Time

	for _, block := range blockchain[1:] {
		blockTimeNano := float64(block.Header.Time.Sub(lastBlockTS))
		blockTime := float64(block.Header.Time.Sub(lastBlockTS) / 1e9) // Convert time from nanoseconds to seconds
		blockSize := float64(block.Size() / (1024))                    // Convert size from bytes to KiB
		thput := blockSize / blockTime

		blockTimesNano = append(blockTimesNano, blockTimeNano)
		blockTimes = append(blockTimes, blockTime)
		blockSizes = append(blockSizes, blockSize)
		throughputs = append(throughputs, thput)

		lastBlockTS = block.Header.Time // update lastBlockTS for the next block
	}
	return blockTimes, blockSizes, throughputs, blockTimesNano
}

func plotData(data []float64, fileName string, title, xLabel, yLabel string) {
	if len(data) == 0 {
		return
	}
	pts := make(plotter.XYs, len(data))
	for i := range data {
		pts[i].X = float64(i)
		pts[i].Y = data[i]
	}

	p, err := plot.New()
	if err != nil {
		panic(err)
	}
	p.Title.Text = title
	p.X.Label.Text = xLabel
	p.Y.Label.Text = yLabel

	err = plotutil.AddLinePoints(p, yLabel, pts)
	if err != nil {
		panic(err)
	}

	// save the plot
	if err := p.Save(10*vg.Inch, 5*vg.Inch, fileName); err != nil {
		panic(err)
	}
}

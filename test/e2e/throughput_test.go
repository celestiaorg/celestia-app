package e2e

import (
	"context"
	"os"
	"testing"
	"time"

	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/plotutil"
	"gonum.org/v1/plot/vg"

	v2 "github.com/celestiaorg/celestia-app/pkg/appconsts/v2"
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
	testnet, err := New(t.Name(), seed)
	require.NoError(t, err)
	t.Cleanup(testnet.Cleanup)

	// add 4 validators
	require.NoError(t, testnet.CreateGenesisNodes(2, latestVersion, 10000000,
		0, Resources{"200Mi", "200Mi", "300m", "200Mi"}))

	// obtain the GRPC endpoints of the validators
	gRPCEndpoints, err := testnet.RemoteGRPCEndpoints()
	require.NoError(t, err)
	t.Log("txsim GRPC endpoint", gRPCEndpoints)

	t.Log("Creating txsim nodes")
	// create txsim nodes and point them to the validators
	txsimVersion := "ebc7f8d" // "65c1a8e" // TODO pull the latest version of txsim if possible

	err = testnet.CreateAndSetupTxSimNodes(txsimVersion, seed, 1, "50000-100000", 3, Resources{"200Mi", "200Mi", "300m", "1Gi"}, gRPCEndpoints)
	require.NoError(t, err)

	// start the testnet
	t.Log("Setting up testnet")
	require.NoError(t, testnet.Setup()) // configs, genesis files, etc.
	t.Log("Starting testnet")
	require.NoError(t, testnet.Start())

	// once the testnet is up, start the txsim
	t.Log("Starting txsim")
	err = testnet.StartTxSimNodes()
	require.NoError(t, err)

	// wait some time for the txsim to submit transactions
	time.Sleep(30 * time.Second)

	t.Log("Reading blockchain")
	blockchain, err := testnode.ReadBlockchain(context.Background(), testnet.Node(0).AddressRPC())
	require.NoError(t, err)

	blockTimes, blockSizes, thputs := throughput(blockchain)
	t.Log("blockTimes", blockTimes)
	t.Log("blockSizes", blockSizes)
	t.Log("thputs", thputs)
	plotData(blockSizes, "blocksizes.png", "Block Size in bytes", "Height",
		"Block Size")
	plotData(blockTimes, "blockTimes.png", "Block Time in seconds", "Height",
		"Block Time in seconds")
	plotData(thputs, "thputs.png", "Throughput in bytes/second",
		"Height", "Throughput in bytes/second")

	totalTxs := 0
	for _, block := range blockchain {
		require.Equal(t, v2.Version, block.Version.App)
		totalTxs += len(block.Data.Txs)
	}
	require.Greater(t, totalTxs, 10)
}

func throughput(blockchain []*types.Block) ([]float64, []float64, []float64) {
	blockTimes := make([]float64, 0, len(blockchain)-1)
	blockSizes := make([]float64, 0, len(blockchain)-1)
	throughputs := make([]float64, 0, len(blockchain)-1)
	// timestamp of the last processed block
	lastBlockTS := blockchain[0].Header.Time

	for _, block := range blockchain[1:] {
		blockTime := float64(block.Header.Time.Sub(lastBlockTS) / 1e9) // Convert time from nanoseconds to seconds
		blockSize := float64(block.Size() / (1024))                    // Convert size from bytes to KiB
		thput := blockSize / blockTime

		blockTimes = append(blockTimes, blockTime)
		blockSizes = append(blockSizes, blockSize)
		throughputs = append(throughputs, thput)

		lastBlockTS = block.Header.Time // update lastBlockTS for the next block
	}
	return blockTimes, blockSizes, throughputs
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

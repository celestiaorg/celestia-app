package e2e

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	v1 "github.com/celestiaorg/celestia-app/pkg/appconsts/v1"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/types"
)

//const seed = 42
//
//var latestVersion = "latest"

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
	err = testnet.SetupTxsimNode("txsim", "65c1a8e",
		testnet.GRPCEndpoints()[0], seed, 5, 50*1024, 3,
		Resources{"200Mi", "200Mi", "300m", "1Gi"},
		txsimRootDir, txsimKeyringDir)
	require.NoError(t, err)

	err = testnet.StartTxSimNode()
	require.NoError(t, err)

	time.Sleep(30 * time.Second)

	blockchain, err := testnode.ReadBlockchain(context.Background(), testnet.Node(0).AddressRPC())
	require.NoError(t, err)

	blockTimes, blockSizes, thputs := throughput(blockchain)
	t.Log("blockTimes", blockTimes)
	t.Log("blockSizes", blockSizes)
	t.Log("thputs", thputs)
	//plotData(blockSizes, "blocksizes.png", "Block Size in bytes", "Height",
	//	"Block Size")
	//plotData(blockTimes, "blockTimes.png", "Block Time in seconds", "Height",
	//	"Block Time in seconds")
	//plotData(thputs, "thputs.png", "Throughput in bytes/second",
	//	"Height", "Throughput in bytes/second")

	totalTxs := 0
	for _, block := range blockchain {
		require.Equal(t, v1.Version, block.Version.App)
		totalTxs += len(block.Data.Txs)
	}
	require.Greater(t, totalTxs, 10)
}

func throughput(blockchain []*types.Block) ([]float64, []float64, []float64) {
	blockTimes := make([]float64, 0, len(blockchain)-1)
	blockSizes := make([]float64, 0, len(blockchain)-1)
	throughputs := make([]float64, 0, len(blockchain)-1)
	// timestamp of the last processed block
	lastBlockTs := blockchain[0].Header.Time

	for _, block := range blockchain[1:] {
		blockTime := float64(block.Header.Time.Sub(lastBlockTs) / 1e9) // Convert time from nanoseconds to seconds
		blockSize := float64(block.Size() / (1024 * 1024))             // Convert size from bytes to MiB
		thput := blockSize / blockTime

		blockTimes = append(blockTimes, blockTime)
		blockSizes = append(blockSizes, blockSize)
		throughputs = append(throughputs, thput)

		lastBlockTs = block.Header.Time // update lastBlockTs for the next block
	}
	return blockTimes, blockSizes, throughputs
}

//func plotData(data []float64, fileName string, title, xLabel,
//	yLabel string,
//) {
//	pts := make(plotter.XYs, len(data))
//	for i := range data {
//		pts[i].X = float64(i)
//		pts[i].Y = data[i]
//	}
//
//	p, err := plot.New()
//	if err != nil {
//		panic(err)
//	}
//	p.Title.Text = title
//	p.X.Label.Text = xLabel
//	p.Y.Label.Text = yLabel
//
//	err = plotutil.AddLinePoints(p, yLabel, pts)
//	if err != nil {
//		panic(err)
//	}
//
//	// save the plot
//	if err := p.Save(10*vg.Inch, 5*vg.Inch, fileName); err != nil {
//		panic(err)
//	}
//}
//
//func TestPlotBlockSize(_ *testing.T) {
//	blockSizes := []float64{100, 200, 300, 400, 500, 600, 700, 800, 900, 1000}
//	plotData(blockSizes, "blocksize.png", "Block Size in bytes",
//		"Block Height", "Size in bytes")
//}

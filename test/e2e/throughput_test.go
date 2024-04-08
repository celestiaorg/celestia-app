package e2e

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/types"
	"gonum.org/v1/plot"
	"gonum.org/v1/plot/plotter"
	"gonum.org/v1/plot/plotutil"
	"gonum.org/v1/plot/vg"
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
	t.Cleanup(func() {
		t.Log("Cleaning up testnet")
		testnet.Cleanup()
	})
	//testnet.genesis.ConsensusParams.Version.AppVersion = testground.Version

	// add 2 validators
	require.NoError(t, testnet.CreateGenesisNodes(2, latestVersion, 10000000,
		0, maxValidatorResources))

	// obtain the GRPC endpoints of the validators
	gRPCEndpoints, err := testnet.RemoteGRPCEndpoints()
	require.NoError(t, err)
	t.Log("validators GRPC endpoints", gRPCEndpoints)

	// create txsim nodes and point them to the validators
	t.Log("Creating txsim nodes")
	// version of the txsim docker image to be used
	txsimVersion := "a92de72"

	// total generated load
	// (assuming one message per 250 ms)
	// blobRange * 4 * sequence number * total_txClients
	// 200KB* 4 * 5 * 2 = 8MB
	err = testnet.CreateTxClients(txsimVersion, 50, "200000-200000",
		maxTxsimResources,
		append(gRPCEndpoints, gRPCEndpoints...))
	require.NoError(t, err)

	// start the testnet
	t.Log("Setting up testnet")
	require.NoError(t, testnet.Setup()) // configs, genesis files, etc.
	t.Log("Starting testnet")
	require.NoError(t, testnet.Start())

	// once the testnet is up, start the txsim
	t.Log("Starting txsim nodes")
	err = testnet.StartTxClients()
	require.NoError(t, err)

	// wait some time for the txsim to submit transactions
	time.Sleep(10 * time.Minute)

	t.Log("Reading blockchain")
	blockchain, err := testnode.ReadBlockchain(context.Background(), testnet.Node(0).AddressRPC())
	require.NoError(t, err)

	blockTimes, blockSizes, thputs, blockTimesNano := throughput(blockchain)
	t.Log("blockTimes", blockTimes)
	t.Log("blockTimesNano", blockTimesNano)
	t.Log("blockSizes", blockSizes)
	t.Log("thputs", thputs)

	// save the data to a CSV file
	err = SaveFloatsToCSV(blockTimes, "blockTimes.csv")
	require.NoError(t, err)
	err = SaveFloatsToCSV(blockTimesNano, "blockTimesNano.csv")
	require.NoError(t, err)
	err = SaveFloatsToCSV(blockSizes, "blockSizes.csv")
	require.NoError(t, err)
	err = SaveFloatsToCSV(thputs, "throughputs.csv")
	require.NoError(t, err)

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
		require.Equal(t, appconsts.LatestVersion, block.Version.App)
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

		blockTimesNano = append(blockTimesNano, blockTimeNano)
		blockTimes = append(blockTimes, blockTime)

		lastBlockTS = block.Header.Time // update lastBlockTS for the next block
	}
	for i, block := range blockchain[:len(blockchain)-1] {
		blockSize := float64(block.Size() / (1024)) // Convert size from bytes to KiB
		thput := blockSize / blockTimes[i]

		blockSizes = append(blockSizes, blockSize)
		throughputs = append(throughputs, thput)
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

// SaveFloatsToCSV saves a slice of float values to a CSV file with the given filename.
func SaveFloatsToCSV(floats []float64, fileName string) error {
	// Create or open the CSV file
	file, err := os.Create(fileName)
	if err != nil {
		return fmt.Errorf("error creating file: %w", err)
	}
	defer file.Close()

	// Create a CSV writer
	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Convert float values to strings and prepare for CSV writing
	var record []string
	for _, value := range floats {
		// Convert each float value to a string
		strValue := strconv.FormatFloat(value, 'f', -1, 64)
		record = append(record, strValue)
	}

	// Write the record (slice of strings) to the CSV
	if err := writer.Write(record); err != nil {
		return fmt.Errorf("error writing record to csv: %w", err)
	}

	return nil // Return nil if no error occurred
}

package e2e

import (
	"bufio"
	"context"
	"encoding/csv"
	"os"
	"strconv"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
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
	// blobRange * 4 (tx per second) * sequence number * total_txClients
	// 200KB* 4 * 40 * 2 = 8MB
	err = testnet.CreateTxClients(txsimVersion, 40, "200000-200000",
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
	//require.NoError(t, err)

	// wait some time for the txsim to submit transactions
	//time.Sleep(10 * time.Minute)

	ticker := time.Tick(5 * time.Minute)
	stop := make(chan struct{})
	go func() {
		reader := bufio.NewReader(os.Stdin)
		for {
			line, _ := reader.ReadString('\n')
			//t.Log("Received signal", string(line))
			if string(line) == "stop" {
				t.Log("stop signal is received")
				close(stop)
				return
			}
		}
	}()
	select {
	case <-ticker:
		t.Log("Time is up")
	case <-stop:
		t.Log("Stopping the test")
	}

	t.Log("Reading blockchain")
	blockchain, err := testnode.ReadBlockchain(context.Background(), testnet.Node(0).AddressRPC())
	require.NoError(t, err)

	// save the blockchain data to a CSV file
	blockData := readBlockData(blockchain)
	err = SaveToCSV(blockData, "./results/blockData.csv")

	totalTxs := 0
	for _, block := range blockchain {
		require.Equal(t, appconsts.LatestVersion, block.Version.App)
		totalTxs += len(block.Data.Txs)
	}
	require.Greater(t, totalTxs, 10)
}

func readBlockData(blockchain []*types.Block) []BlockData {
	blockData := make([]BlockData, 0, len(blockchain))
	for _, block := range blockchain {
		blockData = append(blockData, BlockData{
			Time:   block.Header.Time,
			Size:   float64(block.Size()),
			Height: int(block.Height),
		})
	}
	return blockData

}

type BlockData struct {
	Time   time.Time // Use time.Time for the Time field
	Size   float64   // in bytes
	Height int
}

// Function to save slice of BlockData to CSV
func SaveToCSV(blockData []BlockData, filePath string) error {
	// Create a new file
	file, err := os.Create(filePath)
	if err != nil {
		return err
	}
	defer file.Close()

	// Create a CSV writer
	writer := csv.NewWriter(file)
	defer writer.Flush()

	// Write the header
	header := []string{"Time", "Size", "Height"}
	if err := writer.Write(header); err != nil {
		return err
	}

	// Iterate over the slice and write each element as a row in the CSV
	for _, data := range blockData {
		row := []string{
			data.Time.Format(time.RFC3339), // Format the time using RFC3339 standard
			strconv.FormatFloat(data.Size, 'f', -1, 64),
			strconv.Itoa(data.Height),
		}
		if err := writer.Write(row); err != nil {
			return err
		}
	}

	return nil
}

package cmd

import (
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

func downloadGenesisCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download-genesis chain-id",
		Short: "Download genesis file from https://github.com/celestiaorg/networks",
		Long: "Download genesis file from https://github.com/celestiaorg/networks.\n" +
			"The first argument must be a known chain-id. Ex. celestia, mocha-4, or arabica-10.\n",
		Args: cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			chainID := args[0]
			if !isKnownChainID(chainID) {
				return fmt.Errorf("unknown chain-id: %s. Must be: celestia, mocha-4, or arabica-10.", chainID)
			}
			outputFile := server.GetServerContextFromCmd(cmd).Config.GenesisFile()
			fmt.Printf("Downloading genesis file for %s to %s\n", chainID, outputFile)

			url := fmt.Sprintf("https://raw.githubusercontent.com/celestiaorg/networks/master/%s/genesis.json", chainID)
			if err := downloadFile(outputFile, url); err != nil {
				return fmt.Errorf("error downloading genesis file: %s", err)
			}
			fmt.Printf("Downloaded genesis file for %s to %s\n", chainID, outputFile)
			return nil
		},
	}

	return cmd
}

func isKnownChainID(chainID string) bool {
	knownChainIDs := []string{
		"arabica-10", // testnet
		"mocha-4",    // testnet
		"celestia",   // mainnet
	}
	return contains(knownChainIDs, chainID)
}

// contains checks if a string is present in a slice.
func contains(slice []string, s string) bool {
	for _, v := range slice {
		if v == s {
			return true
		}
	}
	return false
}

// downloadFile will download a URL to a local file. It's efficient because it will
// write as it downloads and not load the whole file into memory.
func downloadFile(filepath string, url string) error {
	resp, err := http.Get(url)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	out, err := os.Create(filepath)
	if err != nil {
		return err
	}
	defer out.Close()

	_, err = io.Copy(out, resp.Body)
	return err
}

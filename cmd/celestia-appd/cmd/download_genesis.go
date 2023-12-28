package cmd

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"

	"github.com/cosmos/cosmos-sdk/client/input"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

const (
	flagYes = "yes"
)

// chainIDToSha256 is a map of chainID to the SHA-256 hash of the genesis file for that chain ID.
// To add a new chain-id, download the genesis file from the networks repo and compute the SHA-256 hash.
// Add the chain-id and hash to this map.
var chainIDToSha256 = map[string]string{
	"celestia":   "9727aac9bbfb021ce7fc695a92f901986421283a891b89e0af97bc9fad187793",
	"mocha-4":    "0846b99099271b240b638a94e17a6301423b5e4047f6558df543d6e91db7e575",
	"arabica-10": "fad0a187669f7a2c11bb07f9dc27140d66d2448b7193e186312713856f28e3e1",
}

func downloadGenesisCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download-genesis [chain-id]",
		Short: "Download genesis file from https://github.com/celestiaorg/networks",
		Long: "Download genesis file from https://github.com/celestiaorg/networks.\n" +
			"The first argument should be a known chain-id. Ex. celestia, mocha-4, or arabica-10.\n" +
			"If no argument is provided, defaults to celestia.\n",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			chainID := getChainIDOrDefault(args)
			if !isKnownChainID(chainID) {
				return fmt.Errorf("unknown chain-id: %s. Must be: celestia, mocha-4, or arabica-10", chainID)
			}
			outputFile := server.GetServerContextFromCmd(cmd).Config.GenesisFile()
			// confirm overwrite, unless -y is passed
			if skip, _ := cmd.Flags().GetBool(flagYes); !skip {
				// if file is existed
				if _, err := os.Stat(outputFile); err == nil {
					buf := bufio.NewReader(cmd.InOrStdin())
					if yes, err := input.GetConfirmation(fmt.Sprintf("A genesis file already exists at %s. Do you want to overwrite it?", outputFile), buf, cmd.ErrOrStderr()); err != nil {
						return fmt.Errorf("get confirmation error: %w", err)
					} else if !yes {
						return nil
					}
				}
			}

			fmt.Printf("Downloading genesis file for %s to %s\n", chainID, outputFile)

			url := fmt.Sprintf("https://raw.githubusercontent.com/celestiaorg/networks/master/%s/genesis.json", chainID)
			if err := downloadFile(outputFile, url); err != nil {
				return fmt.Errorf("error downloading / persisting the genesis file: %s", err)
			}
			fmt.Printf("Downloaded genesis file for %s to %s\n", chainID, outputFile)

			// Compute SHA-256 hash of the downloaded file
			hash, err := computeSha256(outputFile)
			if err != nil {
				return fmt.Errorf("error computing sha256 hash: %s", err)
			}

			// Compare computed hash against known hash
			knownHash, ok := chainIDToSha256[chainID]
			if !ok {
				return fmt.Errorf("unknown chain-id: %s", chainID)
			}

			if hash != knownHash {
				return fmt.Errorf("sha256 hash mismatch: got %s, expected %s", hash, knownHash)
			}

			fmt.Printf("SHA-256 hash verified for %s\n", chainID)
			return nil
		},
	}
	cmd.Flags().BoolP(flagYes, "y", false, "Skip overwrite confirmation prompt when genesis.json already exists")

	return cmd
}

// getChainIDOrDefault returns the chainID from the command line arguments. If
// none is provided, defaults to celestia (mainnet).
func getChainIDOrDefault(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	return "celestia"
}

// isKnownChainID returns true if the chainID is known.
func isKnownChainID(chainID string) bool {
	knownChainIDs := getKeys(chainIDToSha256)
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

// downloadFile will download a URL to a local file.
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

// computeSha256 computes the SHA-256 hash of a file.
func computeSha256(filepath string) (string, error) {
	f, err := os.Open(filepath)
	if err != nil {
		return "", err
	}
	defer f.Close()

	hasher := sha256.New()
	if _, err := io.Copy(hasher, f); err != nil {
		return "", err
	}

	return hex.EncodeToString(hasher.Sum(nil)), nil
}

func getKeys(m map[string]string) (result []string) {
	for key := range m {
		result = append(result, key)
	}
	return result
}

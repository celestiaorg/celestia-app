package cmd

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
)

// chainIDToSha256 is a map of chainID to the SHA-256 hash of the genesis file for that chain ID.
// To add a new chain-id, download the genesis file from the networks repo and compute the SHA-256 hash.
// Add the chain-id and hash to this map.
var chainIDToSha256 = map[string]string{
	appconsts.MainnetChainID: "9727aac9bbfb021ce7fc695a92f901986421283a891b89e0af97bc9fad187793",
	appconsts.MochaChainID:   "82db3ac5ab8485e4784054bd10457533a64563f055c0d234a3134603a9c85d33",
	appconsts.CortoChainID:   "4325c418ae9bd02e5b1c87aef2805243096a1951e7908b464675623f13cba1d2",
}

const downloadGenesisTimeout = 10 * time.Minute

func downloadGenesisCommand() *cobra.Command {
	return newDownloadGenesisCommand(downloadGenesisTimeout, downloadFile)
}

func newDownloadGenesisCommand(timeout time.Duration, download func(context.Context, string, string, string) error) *cobra.Command {
	cmd := &cobra.Command{
		Use:   "download-genesis [chain-id]",
		Short: "Download genesis file from https://github.com/celestiaorg/networks",
		Long: "Download genesis file from https://github.com/celestiaorg/networks.\n" +
			fmt.Sprintf("The first argument should be a known chain-id. Ex. %s\n", chainIDs()) +
			"If no argument is provided, defaults to celestia.\n",
		Args: cobra.MaximumNArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			chainID := getChainIDOrDefault(args)
			knownHash, ok := chainIDToSha256[chainID]
			if !ok {
				return fmt.Errorf("unknown chain-id: %s. Must be: %s", chainID, chainIDs())
			}
			outputFile := server.GetServerContextFromCmd(cmd).Config.GenesisFile()
			fmt.Printf("Downloading genesis file for %s to %s\n", chainID, outputFile)

			url := fmt.Sprintf("https://raw.githubusercontent.com/celestiaorg/networks/master/%s/genesis.json", chainID)
			ctx, cancel := context.WithTimeout(cmd.Context(), timeout)
			defer cancel()
			if err := download(ctx, outputFile, url, knownHash); err != nil {
				return fmt.Errorf("error downloading / persisting the genesis file: %w", err)
			}
			fmt.Printf("Downloaded genesis file for %s to %s\n", chainID, outputFile)

			fmt.Printf("SHA-256 hash verified for %s\n", chainID)
			return nil
		},
	}

	return cmd
}

// getChainIDOrDefault returns the chainID from the command line arguments. If
// none is provided, defaults to celestia (mainnet).
func getChainIDOrDefault(args []string) string {
	if len(args) == 1 {
		return args[0]
	}
	return appconsts.MainnetChainID
}

// isKnownChainID returns true if the chainID is known.
func isKnownChainID(chainID string) bool {
	_, ok := chainIDToSha256[chainID]
	return ok
}

func chainIDs() string {
	return strings.Join(getKeys(chainIDToSha256), ", ")
}

// downloadFile downloads and verifies a URL before replacing the destination.
func downloadFile(ctx context.Context, destination, url, expectedHash string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected HTTP status: %s", resp.Status)
	}

	out, err := os.CreateTemp(filepath.Dir(destination), ".genesis-*")
	if err != nil {
		return err
	}
	temporary := out.Name()
	defer os.Remove(temporary)

	if _, err := io.Copy(out, resp.Body); err != nil {
		out.Close()
		return err
	}
	if err := out.Close(); err != nil {
		return err
	}

	hash, err := computeSha256(temporary)
	if err != nil {
		return err
	}
	if hash != expectedHash {
		return fmt.Errorf("sha256 hash mismatch: got %s, expected %s", hash, expectedHash)
	}
	if err := os.Chmod(temporary, 0o644); err != nil {
		return err
	}
	return os.Rename(temporary, destination)
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

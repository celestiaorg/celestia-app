//go:build nova

package nova

import (
	"bytes"
	"github.com/01builders/nova/appd"
	"github.com/stretchr/testify/require"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestCelestiaAppBinaryIsAvailable(t *testing.T) {
	bz, err := appd.CelestiaApp()
	require.NoError(t, err)
	require.NotNil(t, bz)
	t.Logf("bz len: %d", len(bz))
}

func TestMultiplexerSetup(t *testing.T) {
	// TODO: assumes that a make install has been run prior to test execution
	celestiaBin := "celestia-appd"
	celestiaHome := strings.TrimSpace(execCommand(t, celestiaBin, "config", "home"))
	t.Logf("celestia home: %s", celestiaHome)

	// Set up Celestia config
	execCommand(t, celestiaBin, "config", "set", "client", "chain-id", "local_devnet")
	execCommand(t, celestiaBin, "config", "set", "client", "keyring-backend", "test")
	execCommand(t, celestiaBin, "config", "set", "app", "api.enable", "true")

	// Add Alice's key
	execCommand(t, celestiaBin, "keys", "add", "alice")

	genesisPath := getTestFilePath("multi-plexer-genesis.json")
	require.FileExists(t, genesisPath, "multi-plexer-genesis.json file does not exist.")

	targetGenesisPath := filepath.Join(celestiaHome, "config", "genesis.json")
	t.Logf("target genesis path: %s", targetGenesisPath)

	err := copyFile(genesisPath, targetGenesisPath)
	require.NoError(t, err, "failed to copy genesis file from %s to %s: %w", genesisPath, targetGenesisPath, err)

	execCommand(t, celestiaBin, "passthrough", "v3", "add-genesis-account", "alice", "5000000000utia", "--keyring-backend", "test")
	execCommand(t, celestiaBin, "passthrough", "v3", "gentx", "alice", "1000000utia", "--chain-id", "local_devnet")
	execCommand(t, celestiaBin, "passthrough", "v3", "collect-gentxs")

	// TODO: start via the multi plexer root command.
}

// getTestFilePath constructs an absolute path to a testdata file
// within the same directory as the test file.
func getTestFilePath(filename string) string {
	_, currentFile, _, _ := runtime.Caller(0) // Get the current test file's path
	testDir := filepath.Dir(currentFile)      // Get the directory of the test file
	return filepath.Join(testDir, "testdata", filename)
}

// execCommand runs a command and returns stdout/stderr.
func execCommand(t *testing.T, cmd string, args ...string) string {
	t.Helper()
	var out bytes.Buffer
	command := exec.Command(cmd, args...)
	command.Stdout = &out
	err := command.Run()
	require.NoError(t, err, "command failed: %s\nOutput: %s", cmd, out.String())
	return out.String()
}

// copyFile copies a file from src to dst
func copyFile(src, dst string) error {
	input, err := os.ReadFile(src)
	if err != nil {
		return err
	}
	return os.WriteFile(dst, input, 0777)
}

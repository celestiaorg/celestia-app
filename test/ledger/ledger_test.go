package ledger

import (
	"bytes"
	"os"
	"os/exec"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestLedgerSupport(t *testing.T) {
	t.Run("build with Ledger support", func(t *testing.T) {
		// Generate the binary with Ledger support
		cmd := exec.Command("go", "build", "-tags", "ledger", "-o", "celestia-appd", "../../cmd/celestia-appd")
		err := cmd.Run()
		require.NoError(t, err)

		// Clean up the binary
		// defer os.Remove("celestia-appd")

		// Run the binary
		cmd = exec.Command("./celestia-appd", "keys", "add", "test-key-name", "--ledger")
		var out bytes.Buffer
		cmd.Stderr = &out
		err = cmd.Run()
		require.Error(t, err)

		// Verify the output contains an error message
		want := "Error: failed to generate ledger key: failed to retrieve device: ledger nano S: LedgerHID device (idx 0) not found. Ledger LOCKED OR Other Program/Web Browser may have control of device.\n"
		assert.Equal(t, want, out.String())
	})
	t.Run("build without Ledger support", func(t *testing.T) {
		// Generate the binary without Ledger support
		cmd := exec.Command("go", "build", "-o", "celestia-appd", "../../cmd/celestia-appd")
		err := cmd.Run()
		require.NoError(t, err)

		// Clean up the binary
		defer os.Remove("celestia-appd")

		// Run the binary
		cmd = exec.Command("./celestia-appd", "keys", "add", "test-key-name", "--ledger")
		var out bytes.Buffer
		cmd.Stderr = &out
		err = cmd.Run()
		require.Error(t, err)

		// Verify the output contains an error message
		want := "Error: failed to generate ledger key: failed to retrieve device: ledger nano S: support for ledger devices is not available in this executable\n"
		assert.Equal(t, want, out.String())
	})
}

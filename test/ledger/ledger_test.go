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
	if testing.Short() {
		t.Skip("skipping ledger support test in short mode.")
	}

	type testCase struct {
		name   string
		ledger bool
		want   string
	}

	testCases := []testCase{
		{
			name:   "ledger support enabled",
			ledger: true,
			want:   "Error: failed to generate ledger key: failed to retrieve device: ledger nano S: LedgerHID device (idx 0) not found. Ledger LOCKED OR Other Program/Web Browser may have control of device.\n",
		},
		{
			name:   "ledger support disabled",
			ledger: false,
			want:   "Error: failed to generate ledger key: failed to retrieve device: ledger nano S: support for ledger devices is not available in this executable\n",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var cmd *exec.Cmd
			if tc.ledger {
				// Generate the binary with Ledger support
				cmd = exec.Command("go", "build", "-tags", "ledger", "-o", "celestia-appd", "../../cmd/celestia-appd")
			} else {
				// Generate the binary without Ledger support
				cmd = exec.Command("go", "build", "-o", "celestia-appd", "../../cmd/celestia-appd")
			}
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
			assert.Equal(t, tc.want, out.String())
		})
	}
}

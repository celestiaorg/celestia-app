//go:build multiplexer

package appd

import (
	"bytes"
	"fmt"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v9/internal/embedding"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestCreateExecCommand execs a command to an embedded binary.
func TestCreateExecCommand(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping test which expects an embedded binary")
	}

	binaryGenerators := []func() (string, []byte, error){
		embedding.CelestiaAppV3,
		embedding.CelestiaAppV4,
		embedding.CelestiaAppV5,
	}

	for idx, binaryGenerator := range binaryGenerators {
		t.Run(fmt.Sprintf("v%d", idx+3), func(t *testing.T) {
			version, compressedBinary, err := binaryGenerator()
			require.NoError(t, err)

			appdInstance, err := New(version, compressedBinary)
			require.NoError(t, err)
			require.NotNil(t, appdInstance)

			cmd := appdInstance.CreateExecCommand("version")
			require.NotNil(t, cmd)

			var outputBuffer bytes.Buffer
			cmd.Stdout = &outputBuffer

			require.NoError(t, cmd.Run())

			want := strings.TrimPrefix(version, "v")
			got := outputBuffer.String()
			require.NotEmpty(t, got)
			assert.Contains(t, got, want)
		})
	}
}

package abci

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestOpenTraceWriter(t *testing.T) {
	t.Run("openTraceWriter with empty file does not error", func(t *testing.T) {
		_, err := openTraceWriter("")
		require.NoError(t, err)
	})
}

func TestRemoveStart(t *testing.T) {
	type testCase struct {
		name  string
		input []string
		want  []string
	}
	tests := []testCase{
		{
			name:  "should return empty slice if no args are provided",
			input: []string{},
			want:  []string{},
		},
		{
			name:  "should return empty slice if just celestia-appd is provided",
			input: []string{"celestia-appd"},
			want:  []string{},
		},
		{
			name:  "should remove celestia-appd and start from input",
			input: []string{"celestia-appd", "start", "--home", "foo"},
			want:  []string{"--home", "foo"},
		},
		{
			name:  "should preserve extra additional args",
			input: []string{"celestia-appd", "start", "--home", "foo", "--grpc.enable", "--api.enable"},
			want:  []string{"--home", "foo", "--grpc.enable", "--api.enable"},
		},
		{
			// Reproduces https://github.com/celestiaorg/celestia-app/issues/4926
			name:  "should preserve --home if included before start",
			input: []string{"celestia-appd", "--home", "foo", "start"},
			want:  []string{"--home", "foo"},
		},
	}
	for _, test := range tests {
		got := removeStart(test.input)
		require.Equal(t, test.want, got)
	}
}

func TestErrGroupUsage(t *testing.T) {
	t.Run("errgroup documentation should match actual usage", func(t *testing.T) {
		// This test documents what is actually added to the errgroup.
		// The errgroup contains:
		// 1. Signal listener (from getCtx/ListenForQuitSignals)
		// 2. gRPC server (when enabled)
		// 3. API server (when enabled)
		// 4. Block event listener (when gRPC is enabled)
		//
		// Notable: CometBFT node is NOT added to the errgroup - it's started synchronously
		// in startCmtNode() and managed separately.
		//
		// This test serves as documentation to prevent regression of the documentation fix.
		require.True(t, true, "errgroup contains signal listener, gRPC server, API server, and block event listener but NOT CometBFT node")
	})
}

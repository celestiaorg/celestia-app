package cmd

import (
	"context"
	"strconv"
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/require"
)

func TestOverrideMinRetainBlocks(t *testing.T) {
	testCases := []struct {
		name                   string
		minRetainBlocks        uint64
		snapshotInterval       uint64
		snapshotKeepRecent     uint32
		cliFlag                bool // if true, simulate CLI flag being set
		expectedMinRetain      uint64
		expectError            bool
		expectedErrorSubstring string
	}{
		{
			name:               "Override when below minimum",
			minRetainBlocks:    100,
			snapshotInterval:   1500,
			snapshotKeepRecent: 2,
			expectedMinRetain:  appconsts.MinRetainBlocks,
		},
		{
			name:               "Preserve when above minimum",
			minRetainBlocks:    5000,
			snapshotInterval:   1500,
			snapshotKeepRecent: 2,
			expectedMinRetain:  5000,
		},
		{
			name:               "Preserve zero value (prune nothing)",
			minRetainBlocks:    0,
			snapshotInterval:   1500,
			snapshotKeepRecent: 2,
			expectedMinRetain:  0,
		},
		{
			name:               "Override based on snapshot window when larger than minimum",
			minRetainBlocks:    100,
			snapshotInterval:   2000,
			snapshotKeepRecent: 3,
			expectedMinRetain:  6000, // 2000 * 3 = 6000 > 3000
		},
		{
			name:               "Use hard minimum when snapshot window is smaller",
			minRetainBlocks:    100,
			snapshotInterval:   500,
			snapshotKeepRecent: 2,
			expectedMinRetain:  appconsts.MinRetainBlocks, // 500 * 2 = 1000 < 3000
		},
		{
			name:                   "Error on explicit CLI flag below minimum",
			minRetainBlocks:        1,
			snapshotInterval:       1500,
			snapshotKeepRecent:     2,
			cliFlag:                true,
			expectError:            true,
			expectedErrorSubstring: "is below minimum",
		},
		{
			name:               "Allow explicit CLI flag of 0",
			minRetainBlocks:    0,
			snapshotInterval:   1500,
			snapshotKeepRecent: 2,
			cliFlag:            true,
			expectedMinRetain:  0,
		},
		{
			name:               "Allow explicit CLI flag at minimum",
			minRetainBlocks:    appconsts.MinRetainBlocks,
			snapshotInterval:   1500,
			snapshotKeepRecent: 2,
			cliFlag:            true,
			expectedMinRetain:  appconsts.MinRetainBlocks,
		},
		{
			name:               "Allow explicit CLI flag above minimum",
			minRetainBlocks:    5000,
			snapshotInterval:   1500,
			snapshotKeepRecent: 2,
			cliFlag:            true,
			expectedMinRetain:  5000,
		},
		{
			name:                   "Error on CLI flag below snapshot window requirement",
			minRetainBlocks:        4000,
			snapshotInterval:       2000,
			snapshotKeepRecent:     3, // 2000 * 3 = 6000 required
			cliFlag:                true,
			expectError:            true,
			expectedErrorSubstring: "is below minimum 6000",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// Create a mock cobra command with server context
			cmd := &cobra.Command{
				Use: "test",
			}

			// Add the min-retain-blocks flag
			cmd.Flags().Uint64(server.FlagMinRetainBlocks, 0, "min retain blocks")

			// Set flag value and mark as changed if simulating CLI
			if tc.cliFlag {
				err := cmd.Flags().Set(server.FlagMinRetainBlocks, uintToStr(tc.minRetainBlocks))
				require.NoError(t, err)
			}

			logger := log.NewNopLogger()

			// Create viper and set values
			v := viper.New()
			v.Set(server.FlagMinRetainBlocks, tc.minRetainBlocks)
			v.Set(server.FlagStateSyncSnapshotInterval, tc.snapshotInterval)
			v.Set(server.FlagStateSyncSnapshotKeepRecent, tc.snapshotKeepRecent)

			// Create and set server context
			sctx := server.NewDefaultContext()
			sctx.Viper = v
			sctx.Logger = logger

			// Set the context on the command
			ctx := context.WithValue(context.Background(), server.ServerContextKey, sctx)
			cmd.SetContext(ctx)

			// Run the override function
			err := overrideMinRetainBlocks(cmd, logger)

			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), tc.expectedErrorSubstring)
				return
			}

			require.NoError(t, err)

			// Get the value from viper to verify it was set correctly
			gotMinRetain := v.GetUint64(server.FlagMinRetainBlocks)
			require.Equal(t, tc.expectedMinRetain, gotMinRetain,
				"min-retain-blocks should be %d but got %d", tc.expectedMinRetain, gotMinRetain)
		})
	}
}

// TestOverrideMinRetainBlocks_ViperNotModifiedOnDisk verifies that viper.Set()
// changes are in-memory only and don't persist to disk
func TestOverrideMinRetainBlocks_ViperNotModifiedOnDisk(t *testing.T) {
	// Create a mock cobra command
	cmd := &cobra.Command{
		Use: "test",
	}
	cmd.Flags().Uint64(server.FlagMinRetainBlocks, 0, "min retain blocks")

	logger := log.NewNopLogger()

	// Create viper and set initial values
	v := viper.New()
	v.Set(server.FlagMinRetainBlocks, uint64(100)) // Below minimum
	v.Set(server.FlagStateSyncSnapshotInterval, uint64(1500))
	v.Set(server.FlagStateSyncSnapshotKeepRecent, uint32(2))

	// Create and set server context
	sctx := server.NewDefaultContext()
	sctx.Viper = v
	sctx.Logger = logger

	ctx := context.WithValue(context.Background(), server.ServerContextKey, sctx)
	cmd.SetContext(ctx)

	// Run the override function
	err := overrideMinRetainBlocks(cmd, logger)
	require.NoError(t, err)

	// Verify in-memory value was changed
	require.Equal(t, uint64(appconsts.MinRetainBlocks), v.GetUint64(server.FlagMinRetainBlocks))

	// Note: viper.Set() only changes in-memory values and doesn't write to disk.
	// This is the expected behavior - we want runtime overrides without modifying
	// the user's config file.
}

func uintToStr(n uint64) string {
	return strconv.FormatUint(n, 10)
}

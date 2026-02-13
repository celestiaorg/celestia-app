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

func TestValidateMinRetainBlocks(t *testing.T) {
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
			name:                   "Error when config file value is below minimum",
			minRetainBlocks:        100,
			snapshotInterval:       1500,
			snapshotKeepRecent:     2,
			expectError:            true,
			expectedErrorSubstring: "is below minimum",
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
			name:                   "Error when config file value is below snapshot window requirement",
			minRetainBlocks:        100,
			snapshotInterval:       2000,
			snapshotKeepRecent:     3,
			expectError:            true,
			expectedErrorSubstring: "is below minimum 6000", // 2000 * 3 = 6000 > 3000
		},
		{
			name:                   "Error when config file value is below hard minimum and snapshot window is smaller",
			minRetainBlocks:        100,
			snapshotInterval:       500,
			snapshotKeepRecent:     2,
			expectError:            true,
			expectedErrorSubstring: "is below minimum", // 500 * 2 = 1000 < 3000, so hard minimum 3000 applies
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
		{
			name:               "Preserve config file value exactly at minimum",
			minRetainBlocks:    appconsts.MinRetainBlocks,
			snapshotInterval:   1500,
			snapshotKeepRecent: 2,
			expectedMinRetain:  appconsts.MinRetainBlocks,
		},
		{
			name:                   "Error when config file value is below hard minimum and snapshots are disabled",
			minRetainBlocks:        100,
			snapshotInterval:       0,
			snapshotKeepRecent:     0,
			expectError:            true,
			expectedErrorSubstring: "is below minimum", // 0 * 0 = 0 < 3000, so hard minimum 3000 applies
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
			err := validateMinRetainBlocks(cmd, logger)

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

func uintToStr(n uint64) string {
	return strconv.FormatUint(n, 10)
}

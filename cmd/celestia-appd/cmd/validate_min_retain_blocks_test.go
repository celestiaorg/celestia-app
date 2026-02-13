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
		cliFlag                bool // if true, simulate CLI flag being set
		expectedMinRetain      uint64
		expectError            bool
		expectedErrorSubstring string
	}{
		{
			name:                   "Error when config file value is below minimum",
			minRetainBlocks:        100,
			expectError:            true,
			expectedErrorSubstring: "is below minimum",
		},
		{
			name:                   "Error on explicit CLI flag below minimum",
			minRetainBlocks:        1,
			cliFlag:                true,
			expectError:            true,
			expectedErrorSubstring: "is below minimum",
		},
		{
			name:              "Preserve when above minimum",
			minRetainBlocks:   5000,
			expectedMinRetain: 5000,
		},
		{
			name:              "Preserve zero value (prune nothing)",
			minRetainBlocks:   0,
			expectedMinRetain: 0,
		},
		{
			name:              "Allow explicit CLI flag of 0",
			minRetainBlocks:   0,
			cliFlag:           true,
			expectedMinRetain: 0,
		},
		{
			name:              "Allow explicit CLI flag at minimum",
			minRetainBlocks:   appconsts.MinRetainBlocks,
			cliFlag:           true,
			expectedMinRetain: appconsts.MinRetainBlocks,
		},
		{
			name:              "Allow explicit CLI flag above minimum",
			minRetainBlocks:   5000,
			cliFlag:           true,
			expectedMinRetain: 5000,
		},
		{
			name:              "Preserve config file value exactly at minimum",
			minRetainBlocks:   appconsts.MinRetainBlocks,
			expectedMinRetain: appconsts.MinRetainBlocks,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{
				Use: "test",
			}

			cmd.Flags().Uint64(server.FlagMinRetainBlocks, 0, "min retain blocks")

			if tc.cliFlag {
				err := cmd.Flags().Set(server.FlagMinRetainBlocks, uintToStr(tc.minRetainBlocks))
				require.NoError(t, err)
			}

			logger := log.NewNopLogger()

			// Create viper and set values
			v := viper.New()
			v.Set(server.FlagMinRetainBlocks, tc.minRetainBlocks)

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

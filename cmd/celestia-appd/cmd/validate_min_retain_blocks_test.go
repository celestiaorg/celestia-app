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
		simulateCliFlag        bool
		expectError            bool
		expectedErrorSubstring string
	}{
		{
			name:                   "Error when config file value is below minimum",
			minRetainBlocks:        appconsts.MinRetainBlocks - 1,
			expectError:            true,
			expectedErrorSubstring: "is below minimum",
		},
		{
			name:                   "Error on CLI flag below minimum",
			minRetainBlocks:        appconsts.MinRetainBlocks - 1,
			simulateCliFlag:        true,
			expectError:            true,
			expectedErrorSubstring: "is below minimum",
		},
		{
			name:            "Allow config file of 0",
			minRetainBlocks: 0,
		},
		{
			name:            "Allow CLI flag of 0",
			minRetainBlocks: 0,
			simulateCliFlag: true,
		},
		{
			name:            "Allow config file at minimum",
			minRetainBlocks: appconsts.MinRetainBlocks,
		},
		{
			name:            "Allow CLI flag at minimum",
			minRetainBlocks: appconsts.MinRetainBlocks,
			simulateCliFlag: true,
		},
		{
			name:            "Allow config file above minimum",
			minRetainBlocks: appconsts.MinRetainBlocks + 1,
		},
		{
			name:            "Allow CLI flag above minimum",
			minRetainBlocks: appconsts.MinRetainBlocks + 1,
			simulateCliFlag: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{
				Use: "test",
			}

			cmd.Flags().Uint64(server.FlagMinRetainBlocks, 0, "min retain blocks")

			if tc.simulateCliFlag {
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
		})
	}
}

func uintToStr(n uint64) string {
	return strconv.FormatUint(n, 10)
}

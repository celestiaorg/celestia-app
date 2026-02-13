package cmd

import (
	"context"
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
		name            string
		minRetainBlocks uint64
		expectError     bool
	}{
		{
			name:            "error when below minimum",
			minRetainBlocks: appconsts.MinRetainBlocks - 1,
			expectError:     true,
		},
		{
			name:            "allow zero (retain all blocks)",
			minRetainBlocks: 0,
		},
		{
			name:            "allow at minimum",
			minRetainBlocks: appconsts.MinRetainBlocks,
		},
		{
			name:            "allow above minimum",
			minRetainBlocks: appconsts.MinRetainBlocks + 1,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "test"}

			v := viper.New()
			v.Set(server.FlagMinRetainBlocks, tc.minRetainBlocks)

			sctx := server.NewDefaultContext()
			sctx.Viper = v

			ctx := context.WithValue(context.Background(), server.ServerContextKey, sctx)
			cmd.SetContext(ctx)

			err := validateMinRetainBlocks(cmd, log.NewNopLogger())

			if tc.expectError {
				require.Error(t, err)
				require.Contains(t, err.Error(), "is below minimum")
				return
			}
			require.NoError(t, err)
		})
	}
}

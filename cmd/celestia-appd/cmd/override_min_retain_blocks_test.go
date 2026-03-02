package cmd

import (
	"context"
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverrideMinRetainBlocks(t *testing.T) {
	testCases := []struct {
		name            string
		minRetainBlocks uint64
		want            uint64
	}{
		{
			name:            "override 1 to minimum",
			minRetainBlocks: 1,
			want:            appconsts.MinRetainBlocks,
		},
		{
			name:            "override 2999 to minimum",
			minRetainBlocks: 2999,
			want:            appconsts.MinRetainBlocks,
		},
		{
			name:            "zero is unchanged (retain all blocks)",
			minRetainBlocks: 0,
			want:            0,
		},
		{
			name:            "at minimum is unchanged",
			minRetainBlocks: appconsts.MinRetainBlocks,
			want:            appconsts.MinRetainBlocks,
		},
		{
			name:            "above minimum is unchanged",
			minRetainBlocks: appconsts.MinRetainBlocks + 1,
			want:            appconsts.MinRetainBlocks + 1,
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

			err := overrideMinRetainBlocks(cmd, log.NewNopLogger())
			require.NoError(t, err)
			assert.Equal(t, tc.want, v.GetUint64(server.FlagMinRetainBlocks))
		})
	}
}

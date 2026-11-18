package cmd

import (
	"context"
	"testing"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestOverrideIAVLCacheSize(t *testing.T) {
	minimum := app.DefaultAppConfig().IAVLCacheSize

	testCases := []struct {
		name          string
		iavlCacheSize uint64
		bypass        bool
		want          uint64
	}{
		{
			name:          "override 0 to minimum",
			iavlCacheSize: 0,
			want:          minimum,
		},
		{
			name:          "override SDK default to minimum",
			iavlCacheSize: 781_250,
			want:          minimum,
		},
		{
			name:          "at minimum is unchanged",
			iavlCacheSize: minimum,
			want:          minimum,
		},
		{
			name:          "above minimum is unchanged",
			iavlCacheSize: minimum + 1,
			want:          minimum + 1,
		},
		{
			name:          "bypass flag keeps a lower value",
			iavlCacheSize: 781_250,
			bypass:        true,
			want:          781_250,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			cmd := &cobra.Command{Use: "test"}
			cmd.Flags().Bool(bypassOverridesFlagKey, tc.bypass, "bypass all config overrides")

			v := viper.New()
			v.Set(server.FlagIAVLCacheSize, tc.iavlCacheSize)

			sctx := server.NewDefaultContext()
			sctx.Viper = v

			ctx := context.WithValue(context.Background(), server.ServerContextKey, sctx)
			cmd.SetContext(ctx)

			err := overrideIAVLCacheSize(cmd, log.NewNopLogger())
			require.NoError(t, err)
			assert.Equal(t, tc.want, v.GetUint64(server.FlagIAVLCacheSize))
		})
	}
}

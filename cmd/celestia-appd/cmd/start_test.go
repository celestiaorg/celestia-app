package cmd

import (
	"context"
	"testing"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/server"
	srvrtypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestStart(t *testing.T) {
	tests := []struct {
		name           string
		chainID        string
		expectedHeight int64
	}{
		{
			name:           "ArabicaChainID",
			chainID:        appconsts.ArabicaChainID,
			expectedHeight: appconsts.ArabicaUpgradeHeightV2,
		},
		{
			name:           "MochaChainID",
			chainID:        appconsts.MochaChainID,
			expectedHeight: appconsts.MochaUpgradeHeightV2,
		},
		{
			name:           "MainnetChainID",
			chainID:        appconsts.MainnetChainID,
			expectedHeight: appconsts.MainnetUpgradeHeightV2,
		},
		{
			name:           "UnknownChainID",
			chainID:        "unknown-chain-id",
			expectedHeight: 0,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appCreator := noOpAppCreator()
			cmd := startCmd(appCreator, "/tmp")

			cmd.SetContext(context.Background())
			context, err := client.GetClientQueryContext(cmd)
			require.NoError(t, err)
			context = context.WithChainID(tt.chainID)
			client.SetCmdClientContext(cmd, context)
			serverCtx := server.NewDefaultContext()
			server.SetCmdServerContext(cmd, serverCtx)

			cmd.ExecuteContext(context).RunE(cmd, []string{})

			got := server.GetServerContextFromCmd(cmd)
			assert.Equal(t, tt.expectedHeight, got.Viper.GetInt64(UpgradeHeightFlag))
		})
	}
}

func noOpAppCreator() srvrtypes.AppCreator {
	return testnode.DefaultAppCreator()
}

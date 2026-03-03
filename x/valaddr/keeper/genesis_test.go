//go:build valaddr_wiring

package keeper_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v8/app"
	testutil "github.com/celestiaorg/celestia-app/v8/test/util"
	"github.com/celestiaorg/celestia-app/v8/x/valaddr"
	"github.com/celestiaorg/celestia-app/v8/x/valaddr/types"
	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	t.Run("default genesis", func(t *testing.T) {
		genesis := valaddr.DefaultGenesisState()
		require.NotNil(t, genesis)
	})

	t.Run("validate genesis", func(t *testing.T) {
		tests := []struct {
			name      string
			genesis   *types.GenesisState
			expectErr bool
		}{
			{
				name:      "valid genesis",
				genesis:   valaddr.DefaultGenesisState(),
				expectErr: false,
			},
			{
				name:      "nil genesis",
				genesis:   nil,
				expectErr: true,
			},
		}

		for _, tc := range tests {
			t.Run(tc.name, func(t *testing.T) {
				err := valaddr.ValidateGenesis(tc.genesis)
				if tc.expectErr {
					require.Error(t, err)
				} else {
					require.NoError(t, err)
				}
			})
		}
	})

	t.Run("init genesis", func(t *testing.T) {
		testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
		ctx := testApp.NewContext(true)
		keeper := testApp.ValAddrKeeper

		genesisState := &types.GenesisState{}

		valaddr.InitGenesis(ctx, keeper, genesisState)
	})

	t.Run("export genesis", func(t *testing.T) {
		testApp, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
		ctx := testApp.NewContext(true)
		keeper := testApp.ValAddrKeeper

		exported := valaddr.ExportGenesis(ctx, keeper)

		require.NotNil(t, exported)
	})
}

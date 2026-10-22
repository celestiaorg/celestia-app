package cmd

import (
	"context"
	"fmt"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	cometbftdb "github.com/cometbft/cometbft-db"
	cmtcfg "github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/state"
	"github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/spf13/cobra"
	"github.com/stretchr/testify/require"
)

func TestCheckRollbackAppVersion(t *testing.T) {
	tests := []struct {
		name    string
		version uint64 // 0 means no state
		wantErr string
	}{
		{name: "no state"},
		{name: "older", version: appconsts.Version - 1, wantErr: fmt.Sprintf("passthrough v%d rollback", appconsts.Version-1)},
		{name: "native", version: appconsts.Version},
		{name: "newer", version: appconsts.Version + 1, wantErr: "newer"},
	}
	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			cfg := cmtcfg.DefaultConfig()
			cfg.SetRoot(t.TempDir())
			if tc.version != 0 {
				saveStateAppVersion(t, cfg, tc.version)
			}

			sctx := server.NewDefaultContext()
			sctx.Config = cfg
			cmd := &cobra.Command{}
			cmd.SetContext(context.WithValue(context.Background(), server.ServerContextKey, sctx))

			err := checkRollbackAppVersion(cmd, nil)
			if tc.wantErr == "" {
				require.NoError(t, err)
				return
			}
			require.ErrorContains(t, err, tc.wantErr)
		})
	}
}

// saveStateAppVersion sets the app version of the CometBFT state, creating a minimal state if there is none.
func saveStateAppVersion(t *testing.T, cfg *cmtcfg.Config, version uint64) {
	t.Helper()
	db, err := cometbftdb.NewDB("state", cometbftdb.BackendType(cfg.DBBackend), cfg.DBDir())
	require.NoError(t, err)
	stateStore := state.NewStore(db, state.StoreOptions{})
	defer func() { require.NoError(t, stateStore.Close()) }()

	st, err := stateStore.Load()
	require.NoError(t, err)
	if st.IsEmpty() {
		valSet, _ := types.RandValidatorSet(1, 10)
		st = state.State{
			ChainID:                          "test-chain",
			InitialHeight:                    1,
			LastValidators:                   valSet,
			Validators:                       valSet,
			NextValidators:                   valSet,
			LastHeightValidatorsChanged:      1,
			ConsensusParams:                  *types.DefaultConsensusParams(),
			LastHeightConsensusParamsChanged: 1,
		}
	}
	st.Version.Consensus.App = version
	require.NoError(t, stateStore.Save(st))
}

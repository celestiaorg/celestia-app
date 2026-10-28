package genesis_test

import (
	"encoding/json"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/test/util/genesis"
	zkismtypes "github.com/celestiaorg/celestia-app/v11/x/zkism/types"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	coretypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

// TestExportBytesZkismV11Fields checks that genesis documents for app
// versions below 11 omit the zkism genesis fields introduced in v11, which the
// embedded v10 binary rejects as unknown, while the latest version keeps them.
func TestExportBytesZkismV11Fields(t *testing.T) {
	tests := []struct {
		appVersion    uint64
		wantV11Fields bool
	}{
		{appVersion: 10, wantV11Fields: false},
		{appVersion: appconsts.Version, wantV11Fields: true},
	}

	for _, tc := range tests {
		g := genesis.NewDefaultGenesis().
			WithAppVersion(tc.appVersion).
			WithValidators(genesis.NewDefaultValidator("validator-0"))

		bz, err := g.ExportBytes()
		require.NoError(t, err)

		var doc coretypes.GenesisDoc
		require.NoError(t, cmtjson.Unmarshal(bz, &doc))

		var appState map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(doc.AppState, &appState))

		var zkismState map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(appState[zkismtypes.ModuleName], &zkismState))

		for _, field := range []string{"tree_states", "state_root_history", "tree_state_history"} {
			_, has := zkismState[field]
			require.Equal(t, tc.wantV11Fields, has, "app version %d, field %s", tc.appVersion, field)
		}
		require.Contains(t, zkismState, "isms", "app version %d", tc.appVersion)
		require.Contains(t, zkismState, "submissions", "app version %d", tc.appVersion)
	}
}

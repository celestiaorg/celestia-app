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

// TestExportBytesZkismTreeStates checks that genesis documents for app
// versions below 11 omit the zkism tree_states field, which the embedded v10
// binary rejects as unknown, while the latest version keeps it.
func TestExportBytesZkismTreeStates(t *testing.T) {
	tests := []struct {
		appVersion     uint64
		wantTreeStates bool
	}{
		{appVersion: 10, wantTreeStates: false},
		{appVersion: appconsts.Version, wantTreeStates: true},
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

		_, hasTreeStates := zkismState["tree_states"]
		require.Equal(t, tc.wantTreeStates, hasTreeStates, "app version %d", tc.appVersion)
		require.Contains(t, zkismState, "isms", "app version %d", tc.appVersion)
		require.Contains(t, zkismState, "submissions", "app version %d", tc.appVersion)
	}
}

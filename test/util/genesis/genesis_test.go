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

// TestExportBytesZkismV11Fields checks that the v11 zkism genesis fields are
// only exported for app version 11 and above.
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
			WithValidators(genesis.NewDefaultValidator("validator-0")).
			WithModifiers(seedZkism)

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

		var isms []map[string]json.RawMessage
		require.NoError(t, json.Unmarshal(zkismState["isms"], &isms))
		require.Len(t, isms, 1, "app version %d", tc.appVersion)
		for _, field := range []string{"root_history_size", "state_height"} {
			_, has := isms[0][field]
			require.Equal(t, tc.wantV11Fields, has, "app version %d, ism field %s", tc.appVersion, field)
		}
		require.Contains(t, zkismState, "submissions", "app version %d", tc.appVersion)
	}
}

// seedZkism adds one ISM to the zkism genesis state so the export covers the per-ISM fields.
func seedZkism(state map[string]json.RawMessage) map[string]json.RawMessage {
	var zkismState map[string]json.RawMessage
	if err := json.Unmarshal(state[zkismtypes.ModuleName], &zkismState); err != nil {
		panic(err)
	}
	zkismState["isms"] = json.RawMessage(`[{"id":"0x726f757465725f69736d000000000000000000000000002a0000000000000001","owner":"","state":"AA==","merkle_tree_address":"AA==","groth16_vkey":"AA==","state_transition_vkey":"AA==","state_membership_vkey":"AA==","root_history_size":64,"state_height":"7"}]`)
	bz, err := json.Marshal(zkismState)
	if err != nil {
		panic(err)
	}
	state[zkismtypes.ModuleName] = bz
	return state
}

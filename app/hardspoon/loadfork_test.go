package hardspoon_test

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v9/app/hardspoon"
	cmtjson "github.com/cometbft/cometbft/libs/json"
	cmttypes "github.com/cometbft/cometbft/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	"github.com/stretchr/testify/require"
)

// The two layouts a genesis reaches hardspoon in disagree on how 64-bit
// integers are written, and the disagreement is not uniform within a single
// file:
//
//   - `export` marshals the whole AppGenesis with encoding/json, so
//     initial_height is a bare number, but its consensus block goes through
//     ConsensusGenesis.MarshalJSON, which uses cmtjson, so the params inside it
//     are quoted strings.
//   - CometBFT writes genesis.json entirely with cmtjson, so every int64 in it,
//     initial_height included, is a quoted string.
//
// Decoding either layout with a single codec fails on half the file, so these
// tests pin down that both parse.
func TestLoadFork(t *testing.T) {
	params := cmttypes.DefaultConsensusParams()
	params.Block.MaxBytes = 33_554_432
	params.Evidence.MaxAgeNumBlocks = 559_940
	appState := json.RawMessage(`{"auth":{"accounts":[]},"bank":{}}`)

	appGenesis := func(initialHeight int64) *genutiltypes.AppGenesis {
		return &genutiltypes.AppGenesis{
			AppName:       "celestia-appd",
			GenesisTime:   time.Date(2023, 9, 6, 3, 15, 51, 0, time.UTC),
			ChainID:       "mocha-4",
			InitialHeight: initialHeight,
			AppState:      appState,
			Consensus:     genutiltypes.NewConsensusGenesis(params.ToProto(), nil),
		}
	}

	tests := map[string]struct {
		raw           func(*testing.T) []byte
		initialHeight int64
	}{
		// What `celestia-appd export --for-zero-height` writes, and the input
		// hardspoon is pointed at.
		"sdk export": {
			raw: func(t *testing.T) []byte {
				raw, err := json.Marshal(appGenesis(0))
				require.NoError(t, err)
				return raw
			},
			initialHeight: 0,
		},
		// What `genesis collect-gentxs` leaves behind: the same layout, indented
		// by AppGenesis.SaveAs.
		"sdk app genesis, indented": {
			raw: func(t *testing.T) []byte {
				raw, err := json.MarshalIndent(appGenesis(1), "", "  ")
				require.NoError(t, err)
				return raw
			},
			initialHeight: 1,
		},
		// What hardspoon itself writes and what `debug convert-genesis` converts
		// back to, so that `hardspoon verify` can read its own output.
		"cometbft genesis doc": {
			raw: func(t *testing.T) []byte {
				raw, err := cmtjson.Marshal(&cmttypes.GenesisDoc{
					GenesisTime:     time.Date(2026, 9, 1, 14, 0, 0, 0, time.UTC),
					ChainID:         "mocha-5",
					InitialHeight:   1,
					ConsensusParams: params,
					AppState:        appState,
				})
				require.NoError(t, err)
				return raw
			},
			initialHeight: 1,
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			fork, err := hardspoon.LoadFork(test.raw(t))
			require.NoError(t, err)

			require.Equal(t, test.initialHeight, fork.InitialHeight)
			require.Contains(t, fork.AppState, "auth")
			require.Contains(t, fork.AppState, "bank")

			require.NotNil(t, fork.ConsensusParams)
			require.Equal(t, int64(33_554_432), fork.ConsensusParams.Block.MaxBytes)
			require.Equal(t, int64(-1), fork.ConsensusParams.Block.MaxGas)
			require.Equal(t, int64(559_940), fork.ConsensusParams.Evidence.MaxAgeNumBlocks)
			require.Equal(t, []string{cmttypes.ABCIPubKeyTypeEd25519}, fork.ConsensusParams.Validator.PubKeyTypes)
		})
	}
}

// TestLoadForkPrefersConsensusGenesis pins down which consensus params win when
// a file carries both. `export` overwrites the consensus block with the live
// params, while the top-level consensus_params it inherited from the node's own
// genesis file may be stale.
func TestLoadForkPrefersConsensusGenesis(t *testing.T) {
	live := cmttypes.DefaultConsensusParams()
	live.Block.MaxBytes = 33_554_432
	stale := cmttypes.DefaultConsensusParams()
	stale.Block.MaxBytes = 2_097_152

	consensus, err := json.Marshal(genutiltypes.NewConsensusGenesis(live.ToProto(), nil))
	require.NoError(t, err)
	staleParams, err := cmtjson.Marshal(stale)
	require.NoError(t, err)

	raw, err := json.Marshal(map[string]json.RawMessage{
		"chain_id":         json.RawMessage(`"mocha-4"`),
		"initial_height":   json.RawMessage(`0`),
		"app_state":        json.RawMessage(`{"auth":{}}`),
		"consensus":        consensus,
		"consensus_params": staleParams,
	})
	require.NoError(t, err)

	fork, err := hardspoon.LoadFork(raw)
	require.NoError(t, err)
	require.Equal(t, int64(33_554_432), fork.ConsensusParams.Block.MaxBytes)
}

func TestLoadForkRejectsIncompleteDocuments(t *testing.T) {
	params, err := cmtjson.Marshal(cmttypes.DefaultConsensusParams())
	require.NoError(t, err)

	tests := map[string]struct {
		raw  string
		want string
	}{
		"no app state": {
			raw:  `{"chain_id":"mocha-4","initial_height":0,"consensus_params":` + string(params) + `}`,
			want: "no app_state",
		},
		"no consensus params": {
			raw:  `{"chain_id":"mocha-4","initial_height":0,"app_state":{"auth":{}}}`,
			want: "no consensus params",
		},
		"not json": {
			raw:  `{"chain_id":`,
			want: "parsing exported genesis",
		},
		"unreadable initial height": {
			raw:  `{"chain_id":"mocha-4","initial_height":"later","app_state":{"auth":{}}}`,
			want: "initial_height",
		},
	}

	for name, test := range tests {
		t.Run(name, func(t *testing.T) {
			_, err := hardspoon.LoadFork([]byte(test.raw))
			require.Error(t, err)
			require.Contains(t, err.Error(), test.want)
		})
	}
}

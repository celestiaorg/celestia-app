package cmd

import (
	"encoding/json"
	"testing"

	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func Test_getConsensusParams(t *testing.T) {
	want := types.ConsensusParams{
		Block:     types.BlockParams{MaxBytes: appconsts.DefaultMaxBytes, MaxGas: -1},
		Evidence:  types.EvidenceParams{MaxAgeNumBlocks: 100000, MaxAgeDuration: 172800000000000, MaxBytes: 1048576},
		Validator: types.ValidatorParams{PubKeyTypes: []string{"ed25519"}},
		Version:   types.VersionParams{App: appconsts.Version},
		ABCI:      types.ABCIParams{VoteExtensionsEnableHeight: 0},
	}
	got := *getConsensusParams()
	assert.Equal(t, want, got)
}

func Test_newPrintInfo(t *testing.T) {
	got, err := json.Marshal(newPrintInfo("moniker", "chain-id", "node-id", "/home"))
	require.NoError(t, err)
	assert.JSONEq(t, `{"moniker":"moniker","chain_id":"chain-id","node_id":"node-id","home":"/home"}`, string(got))
}

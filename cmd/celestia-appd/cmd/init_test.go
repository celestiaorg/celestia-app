package cmd

import (
	"testing"

	"github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/assert"
)

func Test_getConsensusParams(t *testing.T) {
	want := types.ConsensusParams{
		Block:     types.BlockParams{MaxBytes: 22020096, MaxGas: -1},
		Evidence:  types.EvidenceParams{MaxAgeNumBlocks: 100000, MaxAgeDuration: 172800000000000, MaxBytes: 1048576},
		Validator: types.ValidatorParams{PubKeyTypes: []string{"ed25519"}},
		Version:   types.VersionParams{App: 0x6},
		ABCI:      types.ABCIParams{VoteExtensionsEnableHeight: 0},
	}
	got := *getConsensusParams()
	assert.Equal(t, want, got)
}

package abci

import (
	"testing"

	abciv2 "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/assert"
	abciv1 "github.com/tendermint/tendermint/abci/types"
)

func TestConsensusParamsV1ToV2(t *testing.T) {
	t.Run("should return nil if params are nil", func(t *testing.T) {
		got := consensusParamsV1ToV2(nil)
		assert.Nil(t, got)
	})
}

func Test_timeoutInfoV1ToV2(t *testing.T) {
	info := abciv1.TimeoutsInfo{
		TimeoutPropose: 1,
		TimeoutCommit:  2,
	}
	want := abciv2.TimeoutInfo{
		TimeoutPropose: 1,
		TimeoutCommit:  2,
	}
	got := timeoutInfoV1ToV2(info)
	assert.Equal(t, want, got)
}

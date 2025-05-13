package abci

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestConsensusParamsV1ToV2(t *testing.T) {
	t.Run("should return nil if params are nil", func(t *testing.T) {
		got := consensusParamsV1ToV2(nil)
		assert.Nil(t, got)
	})
}

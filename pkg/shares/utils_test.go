package shares

import (
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/types"
)

func FuzzMsgSharesUsed(f *testing.F) {
	f.Add(int(1))
	f.Fuzz(func(t *testing.T, a int) {
		if a < 1 {
			t.Skip()
		}
		ml := MsgSharesUsed(int(a))
		msg := generateRandomMessage(int(a))
		rawShares, err := SplitMessages(0, nil, []types.Message{msg}, false)
		require.NoError(t, err)
		require.Equal(t, len(rawShares), ml)
	})
}

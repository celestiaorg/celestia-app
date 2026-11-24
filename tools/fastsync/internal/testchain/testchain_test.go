package testchain

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestChainIsConsistent(t *testing.T) {
	c := New(t, 6)
	require.EqualValues(t, 6, c.N())
	for h := int64(1); h <= c.N(); h++ {
		hd := c.Header(h)
		require.Equal(t, c.Vals[h].Hash(), []byte(hd.ValidatorsHash))
		require.Equal(t, c.Vals[h+1].Hash(), []byte(hd.NextValidatorsHash))
		require.NoError(t, c.Vals[h].VerifyCommit(ChainID, c.BlockID(h), h, c.Commits[h]))
		if h > 1 {
			require.Equal(t, []byte(c.Header(h-1).Hash()), []byte(hd.LastBlockID.Hash))
			require.False(t, bytes.Equal(c.Vals[h-1].Hash(), c.Vals[h].Hash()), "validator sets should change")
		}
	}
}

package util_test

import (
	"bytes"
	"testing"
	"time"

	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	abci "github.com/cometbft/cometbft/abci/types"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	tmtypes "github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

func TestProposalBlockHashMatchesFinalizeBlock(t *testing.T) {
	plainTx, innerTx := []byte("plain tx"), []byte("inner tx")
	blobTx, err := tmtypes.MarshalBlobTx(innerTx, &tmproto.Blob{NamespaceId: bytes.Repeat([]byte{1}, 28), Data: []byte("data")})
	require.NoError(t, err)
	blockTime := time.Unix(1_700_000_000, 0).UTC()
	header := &tmproto.Header{ChainID: "test", Height: 3, Time: blockTime}

	pp := testutil.WithProposalBlockHash(&abci.RequestProcessProposal{Txs: [][]byte{plainTx, blobTx}, Height: 3, Time: blockTime, Header: header})
	fb := testutil.WithBlockHash(&abci.RequestFinalizeBlock{Txs: [][]byte{plainTx, innerTx}, Height: 3, Time: blockTime, Header: header})
	require.Equal(t, fb.Hash, pp.Hash)

	later := testutil.WithBlockHash(&abci.RequestFinalizeBlock{Txs: [][]byte{plainTx, innerTx}, Height: 3, Time: blockTime.Add(time.Second), Header: header})
	require.NotEqual(t, fb.Hash, later.Hash)
}

package app

import (
	"fmt"
	"testing"
	"time"

	"github.com/golang/protobuf/proto"
	"github.com/stretchr/testify/require"
	tmbytes "github.com/tendermint/tendermint/libs/bytes"
	tmjson "github.com/tendermint/tendermint/libs/json"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/proto/tendermint/version"
	"github.com/tendermint/tendermint/types"
)

func TestDebug(t *testing.T) {
	hash := tmbytes.HexBytes("B12EB4FDE40717285AEDC06FD12AD5CB796B5C8070DC781B4B6E01187F0EE165")
	txs := make([][]byte, 100)
	for i := range txs {
		txs[i] = tmrand.Bytes(19000)
	}
	block := types.Block{
		Header: types.Header{
			Version: version.Consensus{
				Block: 1,
				App:   1,
			},
			ChainID:            "celestia",
			Height:             10,
			Time:               time.Now(),
			LastBlockID:        types.BlockID{Hash: hash},
			LastCommitHash:     hash,
			DataHash:           hash,
			ValidatorsHash:     hash,
			NextValidatorsHash: hash,
			ConsensusHash:      hash,
			AppHash:            hash,
			LastResultsHash:    hash,
			EvidenceHash:       hash,
			ProposerAddress:    hash,
		},
		Data: types.Data{
			Txs: types.ToTxs(txs),
		},
		LastCommit: &types.Commit{
			Height:  10,
			Round:   1,
			BlockID: types.BlockID{Hash: hash},
		},
	}

	jsonbz, err := tmjson.Marshal(&block)
	require.NoError(t, err)
	fmt.Println("json version of the block:", len(jsonbz))

	pb, err := block.ToProto()
	require.NoError(t, err)

	protoBz, err := proto.Marshal(pb)
	require.NoError(t, err)

	fmt.Println("proto buf encoded version of the block:", len(protoBz))
}

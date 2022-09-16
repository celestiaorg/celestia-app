package prove

import (
	"bytes"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/da"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/types"
)

func TestTxInclusion(t *testing.T) {
	typicalBlockData := types.Data{
		Txs:                generateRandomlySizedTxs(100, 500),
		Messages:           generateRandomlySizedMessages(40, 16000),
		OriginalSquareSize: 64,
	}
	lotsOfTxsNoMessages := types.Data{
		Txs:                generateRandomlySizedTxs(1000, 500),
		OriginalSquareSize: 64,
	}
	overlappingSquareSize := 16
	overlappingRowsBlockData := types.Data{
		Txs: types.ToTxs(
			[][]byte{
				tmrand.Bytes(appconsts.CompactShareContentSize*overlappingSquareSize + 1),
				tmrand.Bytes(10000),
			},
		),
		OriginalSquareSize: uint64(overlappingSquareSize),
	}
	overlappingRowsBlockDataWithMessages := types.Data{
		Txs: types.ToTxs(
			[][]byte{
				tmrand.Bytes(appconsts.CompactShareContentSize*overlappingSquareSize + 1),
				tmrand.Bytes(10000),
			},
		),
		Messages:           generateRandomlySizedMessages(8, 400),
		OriginalSquareSize: uint64(overlappingSquareSize),
	}

	type test struct {
		data types.Data
	}
	tests := []test{
		{
			typicalBlockData,
		},
		{
			lotsOfTxsNoMessages,
		},
		{
			overlappingRowsBlockData,
		},
		{
			overlappingRowsBlockDataWithMessages,
		},
	}

	for _, tt := range tests {
		for i := 0; i < len(tt.data.Txs); i++ {
			txProof, err := TxInclusion(appconsts.DefaultCodec(), tt.data, uint64(i))
			require.NoError(t, err)
			assert.True(t, txProof.VerifyProof())
		}
	}
}

func TestTxSharePosition(t *testing.T) {
	type test struct {
		name string
		txs  types.Txs
	}

	tests := []test{
		{
			name: "typical",
			txs:  generateRandomlySizedTxs(44, 200),
		},
		{
			name: "many small tx",
			txs:  generateRandomlySizedTxs(444, 100),
		},
		{
			name: "one small tx",
			txs:  generateRandomlySizedTxs(1, 200),
		},
		{
			name: "one large tx",
			txs:  generateRandomlySizedTxs(1, 2000),
		},
		{
			name: "many large txs",
			txs:  generateRandomlySizedTxs(100, 2000),
		},
	}

	type startEndPoints struct {
		start, end uint64
	}

	for _, tt := range tests {
		positions := make([]startEndPoints, len(tt.txs))
		for i := 0; i < len(tt.txs); i++ {
			start, end, err := txSharePosition(tt.txs, uint64(i))
			require.NoError(t, err)
			positions[i] = startEndPoints{start: start, end: end}
		}

		shares := shares.SplitTxs(tt.txs)

		for i, pos := range positions {
			if pos.start == pos.end {
				assert.Contains(t, string(shares[pos.start]), string(tt.txs[i]), tt.name, i, pos)
			} else {
				assert.Contains(
					t,
					joinByteSlices(shares[pos.start:pos.end+1]...),
					string(tt.txs[i]),
					tt.name,
					pos,
					len(tt.txs[i]),
				)
			}
		}
	}
}

func Test_genRowShares(t *testing.T) {
	squareSize := uint64(16)
	typicalBlockData := types.Data{
		Txs:                generateRandomlySizedTxs(10, 200),
		Messages:           generateRandomlySizedMessages(20, 1000),
		OriginalSquareSize: squareSize,
	}

	// note: we should be able to compute row shares from raw data
	// this quickly tests this by computing the row shares before
	// computing the shares in the normal way.
	rowShares, err := genRowShares(
		appconsts.DefaultCodec(),
		typicalBlockData,
		0,
		squareSize,
	)
	require.NoError(t, err)

	rawShares, err := shares.Split(typicalBlockData)
	require.NoError(t, err)

	eds, err := da.ExtendShares(squareSize, rawShares)
	require.NoError(t, err)

	for i := uint64(0); i < squareSize; i++ {
		row := eds.Row(uint(i))
		assert.Equal(t, row, rowShares[i], fmt.Sprintf("row %d", i))
		// also test fetching individual rows
		secondSet, err := genRowShares(appconsts.DefaultCodec(), typicalBlockData, i, i)
		require.NoError(t, err)
		assert.Equal(t, row, secondSet[0], fmt.Sprintf("row %d", i))
	}
}

func Test_genOrigRowShares(t *testing.T) {
	txCount := 100
	squareSize := uint64(16)
	typicalBlockData := types.Data{
		Txs:                generateRandomlySizedTxs(txCount, 200),
		Messages:           generateRandomlySizedMessages(10, 1500),
		OriginalSquareSize: squareSize,
	}

	rawShares, err := shares.Split(typicalBlockData)
	require.NoError(t, err)

	genShares := genOrigRowShares(typicalBlockData, 0, 15)

	require.Equal(t, len(rawShares), len(genShares))
	assert.Equal(t, rawShares, genShares)
}

func joinByteSlices(s ...[]byte) string {
	out := make([]string, len(s))
	for i, sl := range s {
		sl, _, _ := shares.ParseDelimiter(sl)
		out[i] = string(sl[appconsts.NamespaceSize+appconsts.ShareInfoBytes:])
	}
	return strings.Join(out, "")
}

func generateRandomlySizedTxs(count, max int) types.Txs {
	txs := make(types.Txs, count)
	for i := 0; i < count; i++ {
		size := rand.Intn(max)
		if size == 0 {
			size = 1
		}
		txs[i] = generateRandomTxs(1, size)[0]
	}
	return txs
}

func generateRandomTxs(count, size int) types.Txs {
	txs := make(types.Txs, count)
	for i := 0; i < count; i++ {
		tx := make([]byte, size)
		_, err := rand.Read(tx)
		if err != nil {
			panic(err)
		}
		txs[i] = tx
	}
	return txs
}

func generateRandomlySizedMessages(count, maxMsgSize int) types.Messages {
	msgs := make([]types.Message, count)
	for i := 0; i < count; i++ {
		msgs[i] = generateRandomMessage(rand.Intn(maxMsgSize))
	}

	// this is just to let us use assert.Equal
	if count == 0 {
		msgs = nil
	}

	messages := types.Messages{MessagesList: msgs}
	messages.SortMessages()
	return messages
}

func generateRandomMessage(size int) types.Message {
	msg := types.Message{
		NamespaceID: randomValidNamespace(),
		Data:        tmrand.Bytes(size),
	}
	return msg
}

func randomValidNamespace() namespace.ID {
	for {
		s := tmrand.Bytes(8)
		if bytes.Compare(s, appconsts.MaxReservedNamespace) > 0 {
			return s
		}
	}
}

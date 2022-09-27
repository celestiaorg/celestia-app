package prove

import (
	"bytes"
	"fmt"
	"math/rand"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/nmt/namespace"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/types"
)

// func TestTxInclusion(t *testing.T) {
// 	typicalBlockData := types.Data{
// 		Txs:                generateRandomlySizedTxs(100, 500),
// 		Messages:           generateRandomlySizedMessages(40, 16000),
// 		OriginalSquareSize: 64,
// 	}
// 	lotsOfTxsNoMessages := types.Data{
// 		Txs:                generateRandomlySizedTxs(1000, 500),
// 		OriginalSquareSize: 64,
// 	}
// 	overlappingSquareSize := 16
// 	overlappingRowsBlockData := types.Data{
// 		Txs: types.ToTxs(
// 			[][]byte{
// 				tmrand.Bytes(appconsts.ContinuationCompactShareContentSize*overlappingSquareSize + 1),
// 				tmrand.Bytes(10000),
// 			},
// 		),
// 		OriginalSquareSize: uint64(overlappingSquareSize),
// 	}
// 	overlappingRowsBlockDataWithMessages := types.Data{
// 		Txs: types.ToTxs(
// 			[][]byte{
// 				tmrand.Bytes(appconsts.ContinuationCompactShareContentSize*overlappingSquareSize + 1),
// 				tmrand.Bytes(10000),
// 			},
// 		),
// 		Messages:           generateRandomlySizedMessages(8, 400),
// 		OriginalSquareSize: uint64(overlappingSquareSize),
// 	}

// 	type test struct {
// 		data types.Data
// 	}
// 	tests := []test{
// 		{
// 			typicalBlockData,
// 		},
// 		{
// 			lotsOfTxsNoMessages,
// 		},
// 		{
// 			overlappingRowsBlockData,
// 		},
// 		{
// 			overlappingRowsBlockDataWithMessages,
// 		},
// 	}

// 	for _, tt := range tests {
// 		for i := 0; i < len(tt.data.Txs); i++ {
// 			txProof, err := TxInclusion(appconsts.DefaultCodec(), tt.data, uint64(i))
// 			require.NoError(t, err)
// 			assert.True(t, txProof.VerifyProof())
// 		}
// 	}
// }

func TestTxSharePosition(t *testing.T) {
	type test struct {
		name string
		txs  types.Txs
	}

	tests := []test{
		// {
		// 	name: "typical",
		// 	txs:  generateRandomlySizedTxs(44, 200),
		// },
		{
			name: "many small tx",
			txs:  generateRandomlySizedTxs(444, 100),
		},
		// {
		// 	name: "many small tx (without randomness)",
		// 	txs: types.Txs{types.Tx([]byte(`Lorem Ipsum is simply dummy text of the printing and typesetting industry. Lorem Ipsum has been the industry's standard dummy text ever since the 1500s, when an unknown printer took a galley of type and scrambled it to make a type specimen book. It has survived not only five centuries, but also the leap into electronic typesetting, remaining essentially unchanged. It was popularised in the 1960s with the release of Letraset sheets containing Lorem Ipsum passages, and more recently with desktop publishing software like Aldus PageMaker including versions of Lorem Ipsum. orem ipsum dolor sit amet, consectetur adipiscing elit. Sed condimentum orci eu purus tristique tempor. Donec tincidunt dignissim vestibulum. In hac habitasse platea dictumst. Fusce fringilla, purus et eleifend elementum, ex mi volutpat erat, non sodales quam nisl id lacus. Sed at enim nec nunc imperdiet rutrum ac sit amet neque. Pellentesque habitant morbi tristique senectus et netus et malesuada fames ac turpis egestas. Sed fringilla laoreet faucibus. Orci varius natoque penatibus et magnis dis parturient montes, nascetur ridiculus mus. Quisque faucibus, nisi ac gravida consectetur, eros nulla ornare lorem, id scelerisque sapien arcu nec risus. Aliquam consequat, leo sit amet scelerisque egestas, orci sem fringilla nisi, ac sodales tellus justo et lorem. Fusce accumsan, lacus eu imperdiet faucibus, dui est aliquet diam, semper malesuada elit nulla quis arcu. Proin elementum tristique ultricies. Nam luctus lorem sit amet odio eleifend sagittis. Donec hendrerit enim vel est faucibus, nec aliquam ligula lobortis. Fusce elementum, felis id pulvinar fringilla, nisl ipsum ullamcorper felis, in vulputate elit lacus ut nisi. Fusce placerat sit amet diam ac sodales.
		// 	Aliquam eu consequat leo. Integer lacinia mattis nisi nec commodo. Maecenas interdum leo quam, mattis tempus ligula molestie vitae. Proin ut nisl felis. Donec vestibulum a sapien cursus euismod. Vestibulum efficitur non velit vitae venenatis. Donec quis condimentum risus. Morbi leo nibh, luctus ac nisl ornare, interdum facilisis nibh. Fusce lacinia justo felis, vitae venenatis libero pretium ac.
		// 	Cras non lectus vulputate orci pretium mattis id eu nulla. Ut egestas auctor magna, non malesuada erat finibus eu. Proin luctus sapien nec velit tristique, eget molestie enim consequat. Vivamus vel feugiat neque. Aenean vel finibus turpis. Mauris sed nibh purus. Sed vestibulum sed sapien quis posuere. Praesent ornare porttitor laoreet. Nulla facilisi. Sed in tellus hendrerit, euismod velit vitae, elementum ex. Morbi feugiat nisl et lacus tincidunt, vel laoreet velit ultricies.
		// 	Sed ac magna ut mi ornare rutrum ac sit amet sem. In a dolor ut justo imperdiet facilisis nec vitae arcu. Integer sed sapien sed turpis consequat placerat. Cras posuere vel velit ac aliquet. Fusce volutpat sit amet libero et bibendum. Mauris rhoncus enim ipsum, sit amet posuere nibh mollis a. Aliquam tincidunt felis id mauris sagittis, id ultricies ante maximus. Nulla sodales neque vitae convallis rhoncus. Vestibulum facilisis, sem quis iaculis commodo, turpis est mollis justo, faucibus sollicitudin mauris tortor sed quam.
		// 	Nulla in mauris ut ex consequat ultricies non id ipsum. Duis elementum suscipit lorem id tempor. Mauris ultrices justo dui, sed dignissim nisl porta sit amet. Sed auctor felis mauris. Interdum et malesuada fames ac ante ipsum primis in faucibus. Integer eu nunc lacus. Duis posuere lobortis eros. Curabitur aliquam massa et enim auctor, eget faucibus est vehicula.
		// 	`)), types.Tx([]byte("bar")), types.Tx([]byte("buzz"))},
		// },
		// {
		// 	name: "one small tx",
		// 	txs:  generateRandomlySizedTxs(1, 200),
		// },
		// {
		// 	name: "one large tx",
		// 	txs:  generateRandomlySizedTxs(1, 2000),
		// },
		// {
		// 	name: "many large txs",
		// 	txs:  generateRandomlySizedTxs(100, 2000),
		// },
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
			rawTx := []byte(tt.txs[i])
			rawTxDataForRange := stripCompactShares(shares, pos.start, pos.end)
			if !strings.Contains(string(rawTxDataForRange), string(rawTx)) {
				fmt.Printf("txs: %+v\n", tt.txs)
				fmt.Printf("shares: %+v\n", shares)
				fmt.Printf("rawTx: %+v\n", rawTx)
				fmt.Printf("rawTxDataForRange: %+v\n", rawTxDataForRange)
			}
			assert.Contains(
				t,
				string(rawTxDataForRange),
				string(rawTx),
				tt.name,
				pos,
				len(tt.txs[i]),
			)
		}
	}
}

func TestTxShareIndex(t *testing.T) {
	type testCase struct {
		totalTxLen int
		wantIndex  uint64
	}

	tests := []testCase{
		{0, 0},
		{10, 0},
		{100, 0},
		{appconsts.FirstCompactShareContentSize, 0},
		{appconsts.FirstCompactShareContentSize + 1, 1},
		{appconsts.FirstCompactShareContentSize + appconsts.ContinuationCompactShareContentSize, 1},
		{appconsts.FirstCompactShareContentSize + appconsts.ContinuationCompactShareContentSize + 1, 2},
		{appconsts.FirstCompactShareContentSize + (appconsts.ContinuationCompactShareContentSize * 2), 2},
		{appconsts.FirstCompactShareContentSize + (appconsts.ContinuationCompactShareContentSize * 2) + 1, 3},
		// 81 compact shares + partially filled out last share
		{appconsts.FirstCompactShareContentSize + (appconsts.ContinuationCompactShareContentSize * 80) + 160, 81},
		// 81 compact shares + full last share
		{appconsts.FirstCompactShareContentSize + (appconsts.ContinuationCompactShareContentSize * 80) + 246, 81},
		// 82 compact shares + one byte in last share
		{appconsts.FirstCompactShareContentSize + (appconsts.ContinuationCompactShareContentSize * 80) + 247, 82},
		// 82 compact shares + two bytes in last share
		{appconsts.FirstCompactShareContentSize + (appconsts.ContinuationCompactShareContentSize * 80) + 248, 82},
	}

	for _, tt := range tests {
		got := txShareIndex(tt.totalTxLen)
		if got != tt.wantIndex {
			t.Errorf("txShareIndex(%d) got %d, want %d", tt.totalTxLen, got, tt.wantIndex)
		}
	}
}

// TODO: Uncomment/fix this test after we've adjusted tx inclusion proofs to
// work using non-interactive defaults
// func Test_genRowShares(t *testing.T) {
//  squareSize := uint64(16)
//  typicalBlockData := types.Data{
//      Txs:                generateRandomlySizedTxs(10, 200),
//      Messages:           generateRandomlySizedMessages(20, 1000),
//      OriginalSquareSize: squareSize,
//  }

// 	// note: we should be able to compute row shares from raw data
// 	// this quickly tests this by computing the row shares before
// 	// computing the shares in the normal way.
// 	rowShares, err := genRowShares(
// 		appconsts.DefaultCodec(),
// 		typicalBlockData,
// 		0,
// 		squareSize,
// 	)
// 	require.NoError(t, err)

// 	rawShares, err := shares.Split(typicalBlockData, false)
// 	require.NoError(t, err)

// 	eds, err := da.ExtendShares(squareSize, rawShares)
// 	require.NoError(t, err)

// 	for i := uint64(0); i < squareSize; i++ {
// 		row := eds.Row(uint(i))
// 		assert.Equal(t, row, rowShares[i], fmt.Sprintf("row %d", i))
// 		// also test fetching individual rows
// 		secondSet, err := genRowShares(appconsts.DefaultCodec(), typicalBlockData, i, i)
// 		require.NoError(t, err)
// 		assert.Equal(t, row, secondSet[0], fmt.Sprintf("row %d", i))
// 	}
// }

// func Test_genOrigRowShares(t *testing.T) {
// 	txCount := 100
// 	squareSize := uint64(16)
// 	typicalBlockData := types.Data{
// 		Txs:                generateRandomlySizedTxs(txCount, 200),
// 		Messages:           generateRandomlySizedMessages(10, 1500),
// 		OriginalSquareSize: squareSize,
// 	}

// 	rawShares, err := shares.Split(typicalBlockData, false)
// 	require.NoError(t, err)

// 	genShares := genOrigRowShares(typicalBlockData, 0, 15)

// 	require.Equal(t, len(rawShares), len(genShares))
// 	assert.Equal(t, rawShares, genShares)
// }

// stripCompactShares strips the universal prefix (namespace, info byte) and
// reserved byte from a list of compact shares and joins them into a single byte
// slice.
func stripCompactShares(compactShares [][]byte, start uint64, end uint64) (result []byte) {
	for i := start; i <= end; i++ {
		if i == 0 {
			// the first transaction share includes a total data length varint
			result = append(result, compactShares[i][appconsts.NamespaceSize+appconsts.ShareInfoBytes+appconsts.FirstCompactShareDataLengthBytes+appconsts.CompactShareReservedBytes:]...)
		} else {
			result = append(result, compactShares[i][appconsts.NamespaceSize+appconsts.ShareInfoBytes+appconsts.CompactShareReservedBytes:]...)
		}
	}
	return result
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

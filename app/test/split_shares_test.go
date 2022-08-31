package app_test

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/pkg/consts"
	"github.com/tendermint/tendermint/pkg/da"
	core "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	shares "github.com/celestiaorg/celestia-app/pkg/shares"
	"github.com/celestiaorg/celestia-app/testutil"
	"github.com/celestiaorg/celestia-app/x/payment/types"
)

func TestSplitShares(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	type test struct {
		squareSize      uint64
		data            *core.Data
		expectedTxCount int
	}

	signer := testutil.GenerateKeyringSigner(t, testAccName)

	firstNS := []byte{2, 2, 2, 2, 2, 2, 2, 2}
	firstMessage := bytes.Repeat([]byte{4}, 512)
	firstRawTx := generateRawTx(t, encCfg.TxConfig, firstNS, firstMessage, signer, types.AllSquareSizes(len(firstMessage))...)

	secondNS := []byte{1, 1, 1, 1, 1, 1, 1, 1}
	secondMessage := []byte{2}
	secondRawTx := generateRawTx(t, encCfg.TxConfig, secondNS, secondMessage, signer, types.AllSquareSizes(len(secondMessage))...)

	thirdNS := []byte{3, 3, 3, 3, 3, 3, 3, 3}
	thirdMessage := []byte{1}
	invalidSquareSizes := []uint64{2, 8, 16, 32, 64, 128} // missing square size: 4
	thirdRawTx := generateRawTx(t, encCfg.TxConfig, thirdNS, thirdMessage, signer, invalidSquareSizes...)

	tests := []test{
		{
			// calculate the shares using a square size of 4. The third
			// transaction doesn't have a share commit for a square size of 4,
			// so we should expect it to be left out
			squareSize: 4,
			data: &core.Data{
				Txs: [][]byte{firstRawTx, secondRawTx, thirdRawTx},
			},
			expectedTxCount: 2,
		},
		{
			// attempt with only a single tx that can fit in a square of size 2
			squareSize: 2,
			data: &core.Data{
				Txs: [][]byte{secondRawTx},
			},
			expectedTxCount: 1,
		},
		{
			// calculate the square using the same txs but using a square size
			// of 8
			squareSize: 8,
			data: &core.Data{
				Txs: [][]byte{firstRawTx, secondRawTx, thirdRawTx},
			},
			expectedTxCount: 2,
		},
		{
			// calculate the square using the same txs but using a square size
			// of 16
			squareSize: 16,
			data: &core.Data{
				Txs: [][]byte{firstRawTx, secondRawTx, thirdRawTx},
			},
			expectedTxCount: 2,
		},
	}

	for _, tt := range tests {
		square, data := app.SplitShares(encCfg.TxConfig, tt.squareSize, tt.data)

		// has the expected number of txs
		assert.Equal(t, tt.expectedTxCount, len(data.Txs))

		// all shares must be the exect same size
		for _, share := range square {
			assert.Equal(t, consts.ShareSize, len(share))
		}

		// there must be the expected number of shares
		assert.Equal(t, int(tt.squareSize*tt.squareSize), len(square))

		// ensure that the data is written in a way that can be parsed by round
		// tripping
		eds, err := da.ExtendShares(tt.squareSize, square)
		require.NoError(t, err)

		dah := da.NewDataAvailabilityHeader(eds)
		data.Hash = dah.Hash()

		parsedData, err := shares.Merge(eds)
		require.NoError(t, err)

		assert.Equal(t, data.Txs, parsedData.Txs.ToSliceOfBytes())

		parsedShares, err := shares.Split(parsedData)
		require.NoError(t, err)

		require.Equal(t, square, parsedShares)
	}
}

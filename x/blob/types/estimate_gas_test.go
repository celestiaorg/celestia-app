package types_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	testutil "github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/stretchr/testify/require"

	blob "github.com/celestiaorg/celestia-app/x/blob/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/x/auth/signing"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/types"
)

func TestPFBGasEstimation(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	rand := tmrand.NewRand()

	testCases := []struct {
		blobSizes []int
	}{
		{blobSizes: []int{1}},
		{blobSizes: []int{100, 100, 100}},
		{blobSizes: []int{1020, 2099, 96, 4087, 500}},
		{blobSizes: []int{12074}},
		{blobSizes: []int{16908}},
	}
	for idx, tc := range testCases {
		t.Run(fmt.Sprintf("case %d", idx), func(t *testing.T) {
			accnts := testfactory.GenerateAccounts(1)
			testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accnts...)
			signer := blob.NewKeyringSigner(kr, accnts[0], testutil.ChainID)
			blobs := blobfactory.ManyRandBlobs(t, rand, tc.blobSizes...)
			gas := blob.DefaultEstimateGas(toUint32(tc.blobSizes))
			fee := sdk.NewCoins(sdk.NewCoin(app.BondDenom, math.NewInt(int64(gas))))
			tx := blobfactory.MultiBlobTx(t, encCfg.TxConfig.TxEncoder(), signer, 0, 1, blobs, blob.SetGasLimit(gas), blob.SetFeeAmount(fee))
			blobTx, ok := types.UnmarshalBlobTx(tx)
			require.True(t, ok)
			parsedTx, err := encCfg.TxConfig.TxDecoder()(blobTx.Tx)
			require.NoError(t, err)
			fmt.Println(parsedTx.(signing.Tx).GetGas())
			resp := testApp.DeliverTx(abci.RequestDeliverTx{
				Tx: blobTx.Tx,
			})
			require.EqualValues(t, 0, resp.Code, resp.Log)
			require.Less(t, resp.GasUsed, int64(gas))

		})
	}
}

func toUint32(arr []int) []uint32 {
	res := make([]uint32, len(arr))
	for i, v := range arr {
		res[i] = uint32(v)
	}
	return res
}

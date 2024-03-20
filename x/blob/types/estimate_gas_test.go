package types_test

import (
	"fmt"
	"testing"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/test/util"
	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/celestiaorg/go-square/blob"
	"github.com/stretchr/testify/require"

	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
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
		{blobSizes: []int{36908}},
		{blobSizes: []int{100, 100, 100, 1000, 1000, 10000, 100, 100, 100, 100}},
		{blobSizes: []int{1099704}},
	}
	for idx, tc := range testCases {
		t.Run(fmt.Sprintf("case %d", idx), func(t *testing.T) {
			accnts := testfactory.GenerateAccounts(1)
			testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultInitialConsensusParams(), accnts...)
			addr := testfactory.GetAddress(kr, accnts[0])
			signer, err := user.NewSigner(kr, nil, addr, encCfg.TxConfig, testutil.ChainID, 1, 0, appconsts.LatestVersion)
			require.NoError(t, err)
			blobs := blobfactory.ManyRandBlobs(rand, tc.blobSizes...)
			gas := blobtypes.DefaultEstimateGas(toUint32(tc.blobSizes))
			fee := sdk.NewCoins(sdk.NewCoin(app.BondDenom, math.NewInt(int64(gas))))
			tx, err := signer.CreatePayForBlob(blobs, user.SetGasLimit(gas), user.SetFeeAmount(fee))
			require.NoError(t, err)
			blobTx, ok := blob.UnmarshalBlobTx(tx)
			require.True(t, ok)
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

func FuzzPFBGasEstimation(f *testing.F) {
	var (
		numBlobs    = 3
		maxBlobSize = 418
		seed        = int64(9001)
	)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	f.Add(numBlobs, maxBlobSize, seed)
	f.Fuzz(func(t *testing.T, numBlobs, maxBlobSize int, seed int64) {
		if numBlobs <= 0 || maxBlobSize <= 0 {
			t.Skip()
		}
		rand := tmrand.NewRand()
		rand.Seed(seed)
		blobSizes := randBlobSize(rand, numBlobs, maxBlobSize)

		accnts := testfactory.GenerateAccounts(1)
		testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accnts...)
		addr := testfactory.GetAddress(kr, accnts[0])
		signer, err := user.NewSigner(kr, nil, addr, encCfg.TxConfig, testutil.ChainID, 1, 0, appconsts.LatestVersion)
		require.NoError(t, err)
		blobs := blobfactory.ManyRandBlobs(rand, blobSizes...)
		gas := blobtypes.DefaultEstimateGas(toUint32(blobSizes))
		fee := sdk.NewCoins(sdk.NewCoin(app.BondDenom, math.NewInt(int64(gas))))
		tx, err := signer.CreatePayForBlob(blobs, user.SetGasLimit(gas), user.SetFeeAmount(fee))
		require.NoError(t, err)
		blobTx, ok := blob.UnmarshalBlobTx(tx)
		require.True(t, ok)
		resp := testApp.DeliverTx(abci.RequestDeliverTx{
			Tx: blobTx.Tx,
		})
		require.EqualValues(t, 0, resp.Code, resp.Log)
		require.Less(t, resp.GasUsed, int64(gas))
	})
}

func randBlobSize(rand *tmrand.Rand, numBlobs, maxBlobSize int) []int {
	res := make([]int, numBlobs)
	for i := 0; i < numBlobs; i++ {
		if maxBlobSize == 1 {
			res[i] = 1
			continue
		}
		res[i] = rand.Intn(maxBlobSize-1) + 1
	}
	return res
}

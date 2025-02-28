package types_test

import (
	"fmt"
	"math/rand"
	"testing"
	"time"

	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/stretchr/testify/require"

	blobtx "github.com/celestiaorg/go-square/v2/tx"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v4/test/util"
	"github.com/celestiaorg/celestia-app/v4/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/random"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	blobtypes "github.com/celestiaorg/celestia-app/v4/x/blob/types"
)

func TestPFBGasEstimation(t *testing.T) {
	encCfg := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)
	rand := random.New()

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
			signer, err := user.NewSigner(kr, encCfg.TxConfig, testutil.ChainID, appconsts.LatestVersion, user.NewAccount(accnts[0], 1, 0))
			require.NoError(t, err)
			blobs := blobfactory.ManyRandBlobs(rand, tc.blobSizes...)
			gas := blobtypes.DefaultEstimateGas(toUint32(tc.blobSizes))
			tx, _, err := signer.CreatePayForBlobs(accnts[0], blobs, user.SetGasLimitAndGasPrice(gas, appconsts.DefaultMinGasPrice))
			require.NoError(t, err)
			blobTx, ok, err := blobtx.UnmarshalBlobTx(tx)
			require.NoError(t, err)
			require.True(t, ok)
			resp, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
				Txs:    [][]byte{blobTx.Tx},
				Time:   time.Now(),
				Height: 2, // height 1 is genesis
			})
			require.NoError(t, err)
			result := resp.TxResults[0]
			require.EqualValues(t, 0, result.Code, result.Log)
			require.Less(t, result.GasUsed, int64(gas))
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
	encCfg := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)
	f.Add(numBlobs, maxBlobSize, seed)
	f.Fuzz(func(t *testing.T, numBlobs, maxBlobSize int, seed int64) {
		if numBlobs <= 0 || maxBlobSize <= 0 {
			t.Skip()
		}
		blobSizes := randBlobSize(seed, numBlobs, maxBlobSize)

		accnts := testfactory.GenerateAccounts(1)
		testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accnts...)
		signer, err := user.NewSigner(kr, encCfg.TxConfig, testutil.ChainID, appconsts.LatestVersion, user.NewAccount(accnts[0], 1, 0))
		require.NoError(t, err)

		rand := random.New()
		rand.Seed(seed)
		blobs := blobfactory.ManyRandBlobs(rand, blobSizes...)
		gas := blobtypes.DefaultEstimateGas(toUint32(blobSizes))
		tx, _, err := signer.CreatePayForBlobs(accnts[0], blobs, user.SetGasLimitAndGasPrice(gas, appconsts.DefaultMinGasPrice))
		require.NoError(t, err)
		blobTx, ok, err := blobtx.UnmarshalBlobTx(tx)
		require.NoError(t, err)
		require.True(t, ok)
		resp, err := testApp.FinalizeBlock(&abci.RequestFinalizeBlock{
			Txs:    [][]byte{blobTx.Tx},
			Time:   time.Now(),
			Height: 2, // height 1 is genesis
		})
		require.NoError(t, err)
		result := resp.TxResults[0]
		require.EqualValues(t, 0, result.Code, result.Log)
		require.Less(t, result.GasUsed, int64(gas))
	})
}

func randBlobSize(seed int64, numBlobs, maxBlobSize int) []int {
	res := make([]int, numBlobs)
	for i := 0; i < numBlobs; i++ {
		if maxBlobSize == 1 {
			res[i] = 1
			continue
		}
		res[i] = rand.New(rand.NewSource(seed)).Intn(maxBlobSize-1) + 1
	}
	return res
}

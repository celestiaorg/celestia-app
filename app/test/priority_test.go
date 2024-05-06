package app_test

import (
	// "encoding/hex"
	"sort"
	// "sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	// "github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/pkg/user"
	// "github.com/celestiaorg/celestia-app/v2/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v2/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	// blobtypes "github.com/celestiaorg/celestia-app/v2/x/blob/types"
	// "github.com/celestiaorg/go-square/namespace"
	sdk "github.com/cosmos/cosmos-sdk/types"
	// "github.com/ethereum/go-ethereum/accounts"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	// abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	rpctypes "github.com/tendermint/tendermint/rpc/core/types"
)

func TestPriorityTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping app/test/priority_test in short mode.")
	}
	suite.Run(t, &PriorityTestSuite{})
}

type PriorityTestSuite struct {
	suite.Suite

	ecfg    encoding.Config
	signers []*user.Signer
	cctx    testnode.Context

	rand *tmrand.Rand
}

func (s *PriorityTestSuite) SetupSuite() {
	t := s.T()

	cfg := testnode.DefaultConfig().
		WithFundedAccounts(testfactory.GenerateAccounts(10)...).
		// use a long block time to guarantee that some transactions are included in the same block
		WithTimeoutCommit(time.Second)

	cctx, _, _ := testnode.NewNetwork(t, cfg)
	s.cctx = cctx
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
	s.rand = tmrand.NewRand()

	require.NoError(t, cctx.WaitForNextBlock())

	for _, acc := range cfg.Genesis.Accounts() {
		addr := sdk.AccAddress(acc.PubKey.Address())
		client, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg, user.WithDefaultAddress(addr))
		require.NoError(t, err)
		s.signers = append(s.signers, client.Signer())
	}
}

// TestPriorityByGasPrice tests that transactions are sorted by gas price when
// they are included in a block. It does this by submitting blobs with random
// gas prices, and then compares the ordering of the transactions after they are
// committed.
// func (s *PriorityTestSuite) TestPriorityByGasPrice() {
// 	t := s.T()

// 	// quickly submit blobs with a random fee

// 	hashes := make(chan string, len(s.signers))
// 	blobSize := uint32(100)
// 	gasLimit := blobtypes.DefaultEstimateGas([]uint32{blobSize})
// 	wg := &sync.WaitGroup{}
// 	for _, signer := range s.signers {
// 		wg.Add(1)
// 		signer := signer // new variable per iteration
// 		go func() {
// 			defer wg.Done()
// 			// ensure that it is greater than the min gas price
// 			gasPrice := float64(s.rand.Intn(1000)+1) * appconsts.DefaultMinGasPrice
// 			blobs := blobfactory.ManyBlobs(s.rand, []namespace.Namespace{namespace.RandomBlobNamespace()}, []int{100})
// 			resp, _, err := signer.CreatePayForBlobs(
// 				s.cctx.GoContext(), signer.Account(string(user.Account.Address())).String(),blobs,
// 				user.SetGasLimitAndFee(gasLimit, gasPrice),
// 			)
// 			require.NoError(t, err)
// 			require.Equal(t, abci.CodeTypeOK, resp.Code, resp.RawLog)
// 			hashes <- resp.TxHash
// 		}()
// 	}

// 	wg.Wait()
// 	close(hashes)

// 	err := s.cctx.WaitForNextBlock()
// 	require.NoError(t, err)

// 	// get the responses for each tx for analysis and sort by height
// 	// note: use rpc types because they contain the tx index
// 	heightMap := make(map[int64][]*rpctypes.ResultTx)
// 	for hash := range hashes {
// 		// use the core rpc type because it contains the tx index
// 		hash, err := hex.DecodeString(hash)
// 		require.NoError(t, err)
// 		coreRes, err := s.cctx.Client.Tx(s.cctx.GoContext(), hash, false)
// 		require.NoError(t, err)
// 		heightMap[coreRes.Height] = append(heightMap[coreRes.Height], coreRes)
// 	}
// 	require.GreaterOrEqual(t, len(heightMap), 1)

// 	// check that the transactions in each height are sorted by fee after
// 	// sorting by index
// 	highestNumOfTxsPerBlock := 0
// 	for _, responses := range heightMap {
// 		responses = sortByIndex(responses)
// 		require.True(t, isSortedByFee(t, s.ecfg, responses))
// 		if len(responses) > highestNumOfTxsPerBlock {
// 			highestNumOfTxsPerBlock = len(responses)
// 		}
// 	}

// 	// check that there was at least one block with more than three transactions
// 	// in it. This is more of a sanity check than a test.
// 	require.Greater(t, highestNumOfTxsPerBlock, 3)
// }

func sortByIndex(txs []*rpctypes.ResultTx) []*rpctypes.ResultTx {
	sort.Slice(txs, func(i, j int) bool {
		return txs[i].Index < txs[j].Index
	})
	return txs
}

func isSortedByFee(t *testing.T, ecfg encoding.Config, responses []*rpctypes.ResultTx) bool {
	for i := 0; i < len(responses)-1; i++ {
		if getGasPrice(t, ecfg, responses[i]) <= getGasPrice(t, ecfg, responses[i+1]) {
			return false
		}
	}
	return true
}

func getGasPrice(t *testing.T, ecfg encoding.Config, resp *rpctypes.ResultTx) float64 {
	sdkTx, err := ecfg.TxConfig.TxDecoder()(resp.Tx)
	require.NoError(t, err)
	feeTx := sdkTx.(sdk.FeeTx)
	fee := feeTx.GetFee().AmountOf(app.BondDenom).Uint64()
	gas := feeTx.GetGas()
	price := float64(fee) / float64(gas)
	return price
}

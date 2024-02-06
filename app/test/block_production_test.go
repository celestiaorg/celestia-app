package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/test/util/testnode"
	appns "github.com/celestiaorg/go-square/namespace"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/stretchr/testify/suite"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func TestBlockProductionTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping block production test suite in short mode.")
	}
	suite.Run(t, new(BlockProductionTestSuite))
}

type BlockProductionTestSuite struct {
	suite.Suite

	accounts      []string
	cctx          testnode.Context
	timeoutCommit time.Duration
}

func (s *BlockProductionTestSuite) SetupSuite() {
	t := s.T()
	s.timeoutCommit = 10 * time.Second

	accounts := make([]string, 40)
	for i := 0; i < 40; i++ {
		accounts[i] = tmrand.Str(10)
	}

	cfg := testnode.DefaultConfig().
		WithFundedAccounts(accounts...).
		WithTimeoutCommit(s.timeoutCommit)

	cctx, _, _ := testnode.NewNetwork(t, cfg)
	s.cctx = cctx
	s.accounts = accounts
}

// Test_BlockOneTransactionNonInclusion tests that no transactions can be included in the first block.
func (s *BlockProductionTestSuite) Test_BlockOneTransactionNonInclusion() {
	require := s.Require()
	_, err := s.cctx.PostData(s.accounts[0], flags.BroadcastBlock, appns.RandomBlobNamespace(), tmrand.Bytes(100000))

	// since the block production is delayed by 10 seconds, the transactions
	// posted arrive when the node is still at height 0 (not started height 1
	// yet) this makes the post data fail with the following error: rpc error:
	// code = Unknown desc = codespace sdk code 18: invalid request:
	// failed to load state at height 0; no commit info found (latest height: 0)
	require.Error(err) // change this to require.NoError(err) to see the error
	require.ErrorContains(err, "rpc error: code = Unknown desc = codespace sdk code 18: invalid request: failed to load state at height 0; no commit info found (latest height: 0)")
}

// Test_FirstBlockIsEmpty tests whether the first block is empty.
func (s *BlockProductionTestSuite) Test_FirstBlockIsEmpty() {
	require := s.Require()
	// wait until height 1 before posting transactions
	// otherwise tx submission will fail
	time.Sleep(1 * s.timeoutCommit)
	// send some transactions, these should be included in the second block
	_, err := s.cctx.PostData(s.accounts[0], flags.BroadcastBlock, appns.RandomBlobNamespace(), tmrand.Bytes(100000))
	require.NoError(err)

	// wait for 2*s.timeoutCommit+1*time.Second to ensure that the node is
	// at height 2
	_, err = s.cctx.WaitForHeightWithTimeout(2, 2*s.timeoutCommit+1*time.Second)
	require.NoError(err)

	// fetch the first block
	one := int64(1)
	b1, err := s.cctx.Client.Block(s.cctx.GoContext(), &one)
	require.NoError(err)
	// check whether the first block is empty
	require.True(b1.Block.Header.Height == 1)
	require.Equal(len(b1.Block.Data.Txs), 0)

	// fetch the second block
	two := int64(2)
	b2, err := s.cctx.Client.Block(s.cctx.GoContext(), &two)
	require.NoError(err)
	// check whether the second block is non-empty
	require.True(b2.Block.Header.Height == 2)
	require.True(len(b2.Block.Data.Txs) == 1)
}

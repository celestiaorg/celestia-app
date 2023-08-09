package app

import (
	"testing"
	"time"

	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
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
		WithAccounts(accounts).
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

// Test_Empty_Block_One tests that the first block is empty
func (s *BlockProductionTestSuite) Test_Empty_Block_One() {
	require := s.Require()
	// wait for 2*s.timeoutCommit+1*time.Second to ensure that the node is at height 2
	_, err := s.cctx.WaitForHeightWithTimeout(2, 2*s.timeoutCommit+1*time.Second)
	require.NoError(err)
	// fetch block 1
	one := int64(1)
	b, err := s.cctx.Client.Block(s.cctx.GoContext(), &one)
	require.NoError(err)
	// check whether the first block is empty
	require.True(b.Block.Header.Height == 1)
	require.Equal(len(b.Block.Data.Txs), 0)

}

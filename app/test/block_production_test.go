package app

import (
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/stretchr/testify/suite"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"testing"
	"time"
)

func TestBlockProductionestSuite(t *testing.T) {
	suite.Run(t, new(BlockProductionestSuite))
}

type BlockProductionestSuite struct {
	suite.Suite

	accounts []string
	cctx     testnode.Context
}

func (s *BlockProductionestSuite) SetupSuite() {
	if testing.Short() {
		s.T().Skip("skipping full node integration test in short mode.")
	}
	t := s.T()

	accounts := make([]string, 40)
	for i := 0; i < 40; i++ {
		accounts[i] = tmrand.Str(10)
	}

	cfg := testnode.DefaultConfig().
		WithAccounts(accounts).
		WithCommitTimeout(10 * time.Second)

	cctx, _, _ := testnode.NewNetwork(t, cfg)
	s.cctx = cctx
	s.accounts = accounts
}

func (s *BlockProductionestSuite) Test_PostData() {
	require := s.Require()
	_, err := s.cctx.PostData(s.accounts[0], flags.BroadcastBlock, appns.RandomBlobNamespace(), tmrand.Bytes(100000))
	require.Error(err) // change this to require.NoError(err) to see the exact error message
}

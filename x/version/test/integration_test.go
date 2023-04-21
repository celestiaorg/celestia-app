package _test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/testutil/testnode"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func TestVersionIntegrationTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SDK integration test in short mode.")
	}
	suite.Run(t, new(VersionIntegrationTestSuite))
}

type VersionIntegrationTestSuite struct {
	suite.Suite

	cleanup func() error
	cctx    testnode.Context

	vm map[uint64]int64
}

func (s *VersionIntegrationTestSuite) SetupSuite() {
	t := s.T()
	t.Log("setting up integration test suite")

	// set a custom version map
	vm := map[uint64]int64{
		0: 0,
		1: 10,
		2: 20,
		3: 30,
		4: 40,
		5: 50,
		6: 60,
	}
	s.vm = vm

	tmCfg := testnode.DefaultTendermintConfig()
	tmCfg.Consensus.TimeoutCommit = time.Millisecond * 400

	genState, kr, err := testnode.DefaultGenesisState()
	require.NoError(t, err)

	tmNode, app, cctx, err := testnode.New(t, testnode.DefaultParams(), tmCfg, false, genState, kr, tmrand.Str(6), vm)
	require.NoError(t, err)

	cctx, stopNode, err := testnode.StartNode(tmNode, cctx)
	require.NoError(t, err)

	cctx, cleanupGRPC, err := testnode.StartGRPCServer(app, testnode.DefaultAppConfig(), cctx)
	require.NoError(t, err)

	cleanup := func() error {
		err := stopNode()
		if err != nil {
			return err
		}
		return cleanupGRPC()
	}

	s.cleanup = cleanup
	s.cctx = cctx

	s.Require().NoError(s.cctx.WaitForNextBlock())
}

func (s *VersionIntegrationTestSuite) TearDownSuite() {
	t := s.T()
	t.Log("tearing down integration test suite")
	require.NoError(t, s.cleanup())
}

func (s *VersionIntegrationTestSuite) TestVersionBump() {
	t := s.T()

	// wait until the app version should have changed
	h := int64(12)
	_, err := s.cctx.WaitForHeight(h)
	require.NoError(t, err)
	res, err := s.cctx.Client.Block(s.cctx.GoContext(), &h)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Greater(t, res.Block.Header.Version.App, uint64(0))
}

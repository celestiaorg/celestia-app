package app_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/stretchr/testify/suite"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/user"
)

func TestTimeoutTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping app/test/timeouts_test in short mode.")
	}
	suite.Run(t, &TimeoutTestSuite{})
}

type TimeoutTestSuite struct {
	suite.Suite

	ecfg     encoding.Config
	accounts []string
	cctx     testnode.Context
}

const (
	timeoutPropose = 7 * time.Second
	timeoutCommit  = 8 * time.Second
)

func (s *TimeoutTestSuite) SetupSuite() {
	t := s.T()
	s.accounts = testnode.RandomAccounts(142)

	cfg := testnode.DefaultConfig().WithFundedAccounts(s.accounts...)
	cfg.TmConfig.Consensus.TimeoutPropose = timeoutPropose
	cfg.TmConfig.Consensus.TimeoutCommit = timeoutCommit

	cctx, _, _ := testnode.NewNetwork(t, cfg)

	s.cctx = cctx
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)

	require.NoError(t, cctx.WaitForNextBlock())

	for _, acc := range s.accounts {
		addr := testfactory.GetAddress(s.cctx.Keyring, acc)
		_, _, err := user.QueryAccount(s.cctx.GoContext(), s.cctx.GRPCClient, s.cctx.InterfaceRegistry, addr)
		require.NoError(t, err)
	}
}

// TestConfigTimeoutsOverride verifies that the timeouts specified in the
// configs are not used,
// and instead the timeouts are overridden by the ABCI InitChain response
// depending on the genesis app version.
func (s *TimeoutTestSuite) TestConfigTimeoutsOverride() {
	t := s.T()

	require.NoError(t, s.cctx.WaitForBlocks(1))

	tmNode := s.cctx.GetTMNode()
	configTimeoutPropose := tmNode.Config().Consensus.TimeoutPropose.Seconds()
	configTimeoutCommit := tmNode.Config().Consensus.TimeoutCommit.Seconds()

	// double-check the config timeouts are the same as the ones we set
	assert.Equal(t, timeoutPropose.Seconds(), configTimeoutPropose)
	assert.Equal(t, timeoutCommit.Seconds(), configTimeoutCommit)

	// read the timeouts from the state at height 1 i.e.,
	// the state after applying the genesis file
	state1, err := s.cctx.GetTMNode().ConsensusStateTimeoutsByHeight(1)
	require.NoError(t, err)

	// state timeouts should NOT match the timeouts of the config
	assert.True(t, configTimeoutPropose != state1.TimeoutPropose.Seconds())
	assert.True(t, configTimeoutCommit != state1.TimeoutCommit.Seconds())

	// state timeouts should match the timeouts of the latest version of the app
	assert.Equal(t, appconsts.GetTimeoutCommit(appconsts.LatestVersion), state1.TimeoutCommit)
	assert.Equal(t, appconsts.GetTimeoutPropose(appconsts.LatestVersion), state1.TimeoutPropose)

}

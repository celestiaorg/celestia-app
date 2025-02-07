package testnode

import (
	"fmt"
	"testing"
	"time"

	tmrand "cosmossdk.io/math/unsafe"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/test/util/genesis"
	blobtypes "github.com/celestiaorg/celestia-app/v4/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	abci "github.com/cometbft/cometbft/abci/types"
	tmconfig "github.com/cometbft/cometbft/config"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestIntegrationTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping full node integration test in short mode.")
	}
	suite.Run(t, new(IntegrationTestSuite))
}

type IntegrationTestSuite struct {
	suite.Suite

	accounts []string
	cctx     Context
}

func customTendermintConfig() *tmconfig.Config {
	tmCfg := DefaultTendermintConfig()
	// Override the mempool's MaxTxBytes to allow the testnode to accept a
	// transaction that fills the entire square. Any blob transaction larger
	// than the square size will still fail no matter what.
	maxTxBytes := appconsts.DefaultUpperBoundMaxBytes
	tmCfg.Mempool.MaxTxBytes = maxTxBytes

	// Override the MaxBodyBytes to allow the testnode to accept very large
	// transactions and respond to queries with large responses (200 MiB was
	// chosen only as an arbitrary large number).
	tmCfg.RPC.MaxBodyBytes = 200 * mebibyte

	tmCfg.RPC.TimeoutBroadcastTxCommit = time.Minute
	return tmCfg
}

func (s *IntegrationTestSuite) SetupSuite() {
	t := s.T()
	s.accounts = RandomAccounts(10)

	ecfg := encoding.MakeConfig()
	blobGenState := blobtypes.DefaultGenesis()
	blobGenState.Params.GovMaxSquareSize = uint64(appconsts.DefaultSquareSizeUpperBound)

	cfg := DefaultConfig().
		WithFundedAccounts(s.accounts...).
		WithModifiers(genesis.SetBlobParams(ecfg.Codec, blobGenState.Params)).
		WithTendermintConfig(customTendermintConfig())

	cctx, _, _ := NewNetwork(t, cfg)
	s.cctx = cctx
}

func (s *IntegrationTestSuite) TestPostData() {
	require := s.Require()
	_, err := s.cctx.PostData(s.accounts[0], flags.BroadcastSync, share.RandomBlobNamespace(), tmrand.Bytes(kibibyte))
	require.NoError(err)
}

func (s *IntegrationTestSuite) TestFillBlock() {
	require := s.Require()

	for squareSize := 2; squareSize <= appconsts.DefaultGovMaxSquareSize; squareSize *= 2 {
		resp, err := s.cctx.FillBlock(squareSize, s.accounts[1], flags.BroadcastSync)
		require.NoError(err)

		err = s.cctx.WaitForBlocks(3)
		require.NoError(err, squareSize)

		res, err := QueryWithoutProof(s.cctx.Context, resp.TxHash)
		require.NoError(err, squareSize)
		require.Equal(abci.CodeTypeOK, res.TxResult.Code, squareSize)

		b, err := s.cctx.Client.Block(s.cctx.GoContext(), &res.Height)
		require.NoError(err, squareSize)
		require.Equal(uint64(squareSize), b.Block.SquareSize, squareSize)
	}
}

func (s *IntegrationTestSuite) TestFillBlock_InvalidSquareSizeError() {
	tests := []struct {
		name        string
		squareSize  int
		expectedErr error
	}{
		{
			name:        "when squareSize less than 2",
			squareSize:  0,
			expectedErr: fmt.Errorf("unsupported squareSize: 0"),
		},
		{
			name:        "when squareSize is greater than 2 but not a power of 2",
			squareSize:  18,
			expectedErr: fmt.Errorf("unsupported squareSize: 18"),
		},
		{
			name:       "when squareSize is greater than 2 and a power of 2",
			squareSize: 16,
		},
	}

	for _, tc := range tests {
		s.Run(tc.name, func() {
			_, actualErr := s.cctx.FillBlock(tc.squareSize, s.accounts[2], flags.BroadcastAsync)
			s.Equal(tc.expectedErr, actualErr)
		})
	}
}

// Test_defaultAppVersion tests that the default app version is set correctly in
// testnode node.
func (s *IntegrationTestSuite) Test_defaultAppVersion() {
	t := s.T()
	blockRes, err := s.cctx.Client.Block(s.cctx.GoContext(), nil)
	require.NoError(t, err)
	require.Equal(t, appconsts.LatestVersion, blockRes.Block.Version.App)
}

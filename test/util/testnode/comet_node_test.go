package testnode

import (
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v6/test/util/random"
	blobtypes "github.com/celestiaorg/celestia-app/v6/x/blob/types"
	"github.com/celestiaorg/go-square/v2/share"
	abci "github.com/cometbft/cometbft/abci/types"
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

func (s *IntegrationTestSuite) SetupSuite() {
	t := s.T()
	s.accounts = RandomAccounts(10)

	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	blobGenState := blobtypes.DefaultGenesis()
	blobGenState.Params.GovMaxSquareSize = uint64(appconsts.SquareSizeUpperBound)

	cfg := DefaultConfig().
		WithFundedAccounts(s.accounts...).
		WithModifiers(genesis.SetBlobParams(enc.Codec, blobGenState.Params)).
		WithTimeoutCommit(time.Millisecond * 100)

	cctx, _, _ := NewNetwork(t, cfg)
	s.cctx = cctx
}

func (s *IntegrationTestSuite) TestPostData() {
	require := s.Require()
	_, err := s.cctx.PostData(s.accounts[0], flags.BroadcastSync, share.RandomBlobNamespace(), random.Bytes(kibibyte))
	require.NoError(err)
}

func (s *IntegrationTestSuite) TestFillBlock() {
	require := s.Require()

	for squareSize := 2; squareSize <= 64; squareSize *= 2 {
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
	require.Equal(t, appconsts.Version, blockRes.Block.Version.App)
}

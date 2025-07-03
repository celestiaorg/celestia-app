package app_test

import (
	"context"
	"testing"
	"time"

	apperrors "github.com/celestiaorg/celestia-app/v5/app/errors"
	"github.com/celestiaorg/celestia-app/v5/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v5/pkg/user"
	"github.com/celestiaorg/celestia-app/v5/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v5/test/util/testnode"
	"github.com/celestiaorg/go-square/v2/share"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestBigBlobSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping big blob suite in short mode.")
	}
	suite.Run(t, &BigBlobSuite{})
}

type BigBlobSuite struct {
	suite.Suite

	accounts []string
	cctx     testnode.Context
}

func (s *BigBlobSuite) SetupSuite() {
	t := s.T()

	s.accounts = testfactory.GenerateAccounts(1)

	tmConfig := testnode.DefaultTendermintConfig()
	tmConfig.Mempool.MaxTxBytes = 10 * mebibyte

	cParams := testnode.DefaultConsensusParams()
	cParams.Block.MaxBytes = 10 * mebibyte

	cfg := testnode.DefaultConfig().
		WithFundedAccounts(s.accounts...).
		WithTendermintConfig(tmConfig).
		WithConsensusParams(cParams)

	cctx, _, _ := testnode.NewNetwork(t, cfg)
	s.cctx = cctx

	require.NoError(t, cctx.WaitForNextBlock())
}

// TestBlobExceedsMaxTxSize verifies that submitting a 2 MiB blob hits ErrTxExceedsMaxSize.
func (s *BigBlobSuite) TestBlobExceedsMaxTxSize() {
	t := s.T()

	type testCase struct {
		name         string
		blob         *share.Blob
		expectedCode uint32
		expectedErr  string
	}
	testCases := []testCase{
		{
			name:         "2 MiB blob",
			blob:         newBlobWithSize(2097152),
			expectedCode: apperrors.ErrTxExceedsMaxSize.ABCICode(),
			expectedErr:  apperrors.ErrTxExceedsMaxSize.Error(),
		},
	}

	txClient, err := testnode.NewTxClientFromContext(s.cctx)
	require.NoError(t, err)

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			subCtx, cancel := context.WithTimeout(s.cctx.GoContext(), 30*time.Second)
			defer cancel()
			res, err := txClient.SubmitPayForBlob(subCtx, []*share.Blob{tc.blob}, user.SetGasLimitAndGasPrice(1e9, appconsts.DefaultMinGasPrice))
			require.Error(t, err)
			require.Nil(t, res)
			code := err.(*user.BroadcastTxError).Code
			require.Equal(t, tc.expectedCode, code, err.Error(), tc.expectedErr)
		})
	}
}

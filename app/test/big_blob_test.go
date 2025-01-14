package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	apperrors "github.com/celestiaorg/celestia-app/v3/app/errors"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/pkg/user"
	"github.com/celestiaorg/celestia-app/v3/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/v3/x/blob/types"
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

	ecfg     encoding.Config
	accounts []string
	cctx     testnode.Context
}

func (s *BigBlobSuite) SetupSuite() {
	t := s.T()

	s.accounts = testfactory.GenerateAccounts(1)

	tmConfig := testnode.DefaultTendermintConfig()
	tmConfig.Mempool.MaxTxBytes = 10 * app.Mebibyte

	cParams := testnode.DefaultConsensusParams()
	cParams.Block.MaxBytes = 10 * app.Mebibyte

	cfg := testnode.DefaultConfig().
		WithFundedAccounts(s.accounts...).
		WithTendermintConfig(tmConfig).
		WithConsensusParams(cParams)

	cctx, _, _ := testnode.NewNetwork(t, cfg)
	s.cctx = cctx
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)

	require.NoError(t, cctx.WaitForNextBlock())
}

// TestErrBlobsTooLarge verifies that submitting a ~1.9 MiB blob hits ErrBlobsTooLarge.
func (s *BigBlobSuite) TestErrBlobsTooLarge() {
	t := s.T()

	type testCase struct {
		name string
		blob *share.Blob
		// want is the expected tx response ABCI code.
		want uint32
	}
	testCases := []testCase{
		{
			name: "~ 1.9 MiB blob",
			blob: newBlobWithSize(2_000_000),
			want: blobtypes.ErrBlobsTooLarge.ABCICode(),
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
			require.Equal(t, tc.want, code, err.Error())
		})
	}
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

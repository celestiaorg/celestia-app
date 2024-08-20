package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v2/pkg/user"
	"github.com/celestiaorg/celestia-app/v2/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/v2/x/blob/types"
	"github.com/celestiaorg/go-square/blob"
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
	tmConfig.Mempool.MaxTxBytes = 10 * mebibyte

	cParams := testnode.DefaultConsensusParams()
	cParams.Block.MaxBytes = 10 * mebibyte

	cfg := testnode.DefaultConfig().
		WithFundedAccounts(s.accounts...).
		WithTendermintConfig(tmConfig).
		WithConsensusParams(cParams)

	cctx, _, _ := testnode.NewNetwork(t, cfg)
	s.cctx = cctx
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)

	require.NoError(t, cctx.WaitForNextBlock())
}

// TestErrBlobsTooLarge verifies that submitting a 2 MiB blob hits ErrBlobsTooLarge.
func (s *BigBlobSuite) TestErrBlobsTooLarge() {
	t := s.T()

	type testCase struct {
		name string
		blob *blob.Blob
		// want is the expected tx response ABCI code.
		want uint32
	}
	testCases := []testCase{
		{
			name: "2 mebibyte blob",
			blob: newBlobWithSize(2 * mebibyte),
			want: blobtypes.ErrBlobsTooLarge.ABCICode(),
		},
	}

	txClient, err := testnode.NewTxClientFromContext(s.cctx)
	require.NoError(t, err)

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			subCtx, cancel := context.WithTimeout(s.cctx.GoContext(), 30*time.Second)
			defer cancel()
			res, err := txClient.SubmitPayForBlob(subCtx, []*blob.Blob{tc.blob}, user.SetGasLimitAndGasPrice(1e9, appconsts.DefaultMinGasPrice))
			require.Error(t, err)
			require.Nil(t, res)
			code := err.(*user.BroadcastTxError).Code
			require.Equal(t, tc.want, code, err.Error())
		})
	}
}

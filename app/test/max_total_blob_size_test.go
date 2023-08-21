package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/celestiaorg/celestia-app/pkg/user"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

const (
	mebibyte   = 1_048_576 // one mebibyte in bytes
	squareSize = 64
)

func TestMaxTotalBlobSizeSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping max total blob size suite in short mode.")
	}
	suite.Run(t, &MaxTotalBlobSizeSuite{})
}

type MaxTotalBlobSizeSuite struct {
	suite.Suite

	ecfg     encoding.Config
	accounts []string
	cctx     testnode.Context
}

func (s *MaxTotalBlobSizeSuite) SetupSuite() {
	t := s.T()

	s.accounts = testfactory.GenerateAccounts(1)

	tmConfig := testnode.DefaultTendermintConfig()
	tmConfig.Mempool.MaxTxBytes = 10 * mebibyte

	cParams := testnode.DefaultParams()
	cParams.Block.MaxBytes = 10 * mebibyte

	cfg := testnode.DefaultConfig().
		WithAccounts(s.accounts).
		WithTendermintConfig(tmConfig).
		WithConsensusParams(cParams)

	cctx, _, _ := testnode.NewNetwork(t, cfg)
	s.cctx = cctx
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)

	require.NoError(t, cctx.WaitForNextBlock())
}

// TestSubmitPayForBlob_blobSizes verifies the tx response ABCI code when
// SubmitPayForBlob is invoked with different blob sizes.
func (s *MaxTotalBlobSizeSuite) TestSubmitPayForBlob_blobSizes() {
	t := s.T()

	type testCase struct {
		name string
		blob *tmproto.Blob
		// want is the expected tx response ABCI code.
		want uint32
	}
	testCases := []testCase{
		{
			name: "1 byte blob",
			blob: mustNewBlob(t, 1),
			want: abci.CodeTypeOK,
		},
		{
			name: "1 mebibyte blob",
			blob: mustNewBlob(t, mebibyte),
			want: abci.CodeTypeOK,
		},
		{
			name: "2 mebibyte blob",
			blob: mustNewBlob(t, 2*mebibyte),
			want: blobtypes.ErrTotalBlobSizeTooLarge.ABCICode(),
		},
	}

	signer, err := testnode.NewSignerFromContext(s.cctx, s.accounts[0])
	require.NoError(t, err)

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			blobTx, err := signer.CreatePayForBlob([]*tmproto.Blob{tc.blob}, user.SetGasLimit(1e9))
			require.NoError(t, err)
			subCtx, cancel := context.WithTimeout(s.cctx.GoContext(), 30*time.Second)
			defer cancel()
			res, err := signer.BroadcastTx(subCtx, blobTx)
			require.NoError(t, err)
			require.NotNil(t, res)
			require.Equal(t, tc.want, res.Code, res.Logs)

			sq, err := square.Construct([][]byte{blobTx}, appconsts.LatestVersion, squareSize)
			if tc.want == abci.CodeTypeOK {
				// verify that if the tx was accepted, the blob can fit in a square
				assert.NoError(t, err)
				assert.False(t, sq.IsEmpty())
			} else {
				// verify that if the tx was rejected, the blob can not fit in a square
				assert.Error(t, err)
			}
		})
	}
}

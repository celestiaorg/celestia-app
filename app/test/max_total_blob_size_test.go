package app_test

import (
	"context"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/square"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	sdk_tx "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
	"google.golang.org/grpc"
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

	cParams := testnode.DefaultConsensusParams()
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

	signer := blobtypes.NewKeyringSigner(s.cctx.Keyring, s.accounts[0], s.cctx.ChainID)

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			blobTx := newBlobTx(t, signer, s.cctx.GRPCClient, tc.blob)
			res, err := blobtypes.BroadcastTx(context.TODO(), s.cctx.GRPCClient, sdk_tx.BroadcastMode_BROADCAST_MODE_BLOCK, blobTx)
			require.NoError(t, err)
			require.NotNil(t, res)
			require.Equal(t, tc.want, res.TxResponse.Code, res.TxResponse.Logs)

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

func newBlobTx(t *testing.T, signer *blobtypes.KeyringSigner, conn *grpc.ClientConn, blob *tmproto.Blob) coretypes.Tx {
	addr, err := signer.GetSignerInfo().GetAddress()
	require.NoError(t, err)

	msg, err := blobtypes.NewMsgPayForBlobs(addr.String(), blob)
	require.NoError(t, err)

	err = signer.QueryAccountNumber(context.TODO(), conn)
	require.NoError(t, err)

	options := []blobtypes.TxBuilderOption{blobtypes.SetGasLimit(1e9)} // set gas limit to 1 billion to avoid gas exhaustion
	builder := signer.NewTxBuilder(options...)
	stx, err := signer.BuildSignedTx(builder, msg)
	require.NoError(t, err)

	rawTx, err := signer.EncodeTx(stx)
	require.NoError(t, err)

	blobTx, err := coretypes.MarshalBlobTx(rawTx, blob)
	require.NoError(t, err)

	return blobTx
}

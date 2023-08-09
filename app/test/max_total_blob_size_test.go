package app_test

import (
	"bytes"
	"context"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/celestiaorg/celestia-app/x/blob"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

const (
	mebibyte = 1_048_576 // one mebibyte in bytes
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

	s.accounts = randAccounts(1)
	cfg := testnode.DefaultConfig().WithAccounts(s.accounts)

	cctx, _, _ := testnode.NewNetwork(t, cfg)
	s.cctx = cctx
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)

	require.NoError(t, cctx.WaitForNextBlock())

	for _, account := range s.accounts {
		signer := blobtypes.NewKeyringSigner(s.cctx.Keyring, account, s.cctx.ChainID)
		err := signer.QueryAccountNumber(s.cctx.GoContext(), s.cctx.GRPCClient)
		require.NoError(t, err)
	}
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
		// {
		// 	name:           "2 mebibyte blob",
		// 	blob:           mustNewBlob(t, 2*mebibyte),
		// 	txResponseCode: types.ErrTotalBlobSizeTooLarge.ABCICode(),
		// },
	}

	for _, tc := range testCases {
		s.Run(tc.name, func() {
			signer := blobtypes.NewKeyringSigner(s.cctx.Keyring, s.accounts[0], s.cctx.ChainID)
			options := []blobtypes.TxBuilderOption{blobtypes.SetGasLimit(1_000_000_000)}
			txResp, err := blob.SubmitPayForBlob(context.TODO(), signer, s.cctx.GRPCClient, []*blobtypes.Blob{tc.blob}, options...)

			require.NoError(t, err)
			require.NotNil(t, txResp)
			require.Equal(t, tc.want, txResp.Code, txResp.Logs)
		})
	}
}

func mustNewBlob(t *testing.T, blobSize int) *tmproto.Blob {
	ns1 := appns.MustNewV0(bytes.Repeat([]byte{1}, appns.NamespaceVersionZeroIDSize))
	data := tmrand.Bytes(blobSize)
	result, err := blobtypes.NewBlob(ns1, data, appconsts.ShareVersionZero)
	require.NoError(t, err)
	return result
}

func randAccounts(count int) []string {
	accounts := make([]string, count)
	for i := 0; i < count; i++ {
		accounts[i] = tmrand.Str(20)
	}
	return accounts
}

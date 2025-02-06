package testutil

import (
	"encoding/hex"
	"fmt"
	"os"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	clitestutil "github.com/cosmos/cosmos-sdk/testutil/cli"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/x/blob/types"

	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	paycli "github.com/celestiaorg/celestia-app/v3/x/blob/client/cli"
	"github.com/celestiaorg/go-square/v2/share"
	abci "github.com/cometbft/cometbft/abci/types"
)

// username is used to create a funded genesis account under this name
const username = "test"

type IntegrationTestSuite struct {
	suite.Suite

	cfg *testnode.Config
	ctx testnode.Context
}

// Create a .json file for testing
func createTestFile(t testing.TB, s string, isValid bool) *os.File {
	t.Helper()

	tempdir, err := os.MkdirTemp("", "")
	require.NoError(t, err)
	t.Cleanup(func() { _ = os.RemoveAll(tempdir) })

	var fp *os.File

	if isValid {
		fp, err = os.CreateTemp(tempdir, "*.json")
	} else {
		fp, err = os.CreateTemp(tempdir, "")
	}
	require.NoError(t, err)
	_, err = fp.WriteString(s)

	require.Nil(t, err)

	return fp
}

func NewIntegrationTestSuite(cfg *testnode.Config) *IntegrationTestSuite {
	return &IntegrationTestSuite{cfg: cfg}
}

func (s *IntegrationTestSuite) SetupSuite() {
	s.T().Log("setting up integration test suite")
	s.cfg.WithFundedAccounts(username)

	s.ctx, _, _ = testnode.NewNetwork(s.T(), s.cfg)

	_, err := s.ctx.WaitForHeight(1)
	s.Require().NoError(err)
}

func (s *IntegrationTestSuite) TestSubmitPayForBlob() {
	require := s.Require()

	hexBlob := "0204033704032c0b162109000908094d425837422c2116"

	validBlob := fmt.Sprintf(`
	{
		"Blobs": [
			{
				"namespaceID": "%s",
				"blob": "%s"
			},
			{
				"namespaceID": "%s",
				"blob": "%s"
			}
    	]
	}
	`, hex.EncodeToString(share.RandomBlobNamespaceID()), hexBlob, hex.EncodeToString(share.RandomBlobNamespaceID()), hexBlob)
	validPropFile := createTestFile(s.T(), validBlob, true)
	invalidPropFile := createTestFile(s.T(), validBlob, false)

	testCases := []struct {
		name         string
		args         []string
		expectErr    bool
		expectedCode uint32
		respType     proto.Message
	}{
		{
			name: "single blob valid transaction",
			args: []string{
				hex.EncodeToString(share.RandomBlobNamespaceID()),
				hexBlob,
				fmt.Sprintf("--from=%s", username),
				fmt.Sprintf("--%s=%s", flags.FlagBroadcastMode, flags.BroadcastBlock),
				fmt.Sprintf("--%s=%s", flags.FlagFees, sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, sdk.NewInt(1000))).String()),
				fmt.Sprintf("--%s=true", flags.FlagSkipConfirmation),
			},
			expectErr:    false,
			expectedCode: 0,
			respType:     &sdk.TxResponse{},
		},
		{
			name: "multiple blobs valid transaction",
			args: []string{
				fmt.Sprintf("--from=%s", username),
				fmt.Sprintf("--%s=%s", flags.FlagBroadcastMode, flags.BroadcastBlock),
				fmt.Sprintf("--%s=%s", flags.FlagFees, sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, sdk.NewInt(1000))).String()),
				fmt.Sprintf("--%s=true", flags.FlagSkipConfirmation),
				fmt.Sprintf("--%s=%s", paycli.FlagFileInput, validPropFile.Name()),
			},
			expectErr:    false,
			expectedCode: 0,
			respType:     &sdk.TxResponse{},
		},
		{
			name: "multiple blobs with invalid file path extension",
			args: []string{
				fmt.Sprintf("--from=%s", username),
				fmt.Sprintf("--%s=%s", flags.FlagBroadcastMode, flags.BroadcastBlock),
				fmt.Sprintf("--%s=%s", flags.FlagFees, sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, sdk.NewInt(1000))).String()),
				fmt.Sprintf("--%s=true", flags.FlagSkipConfirmation),
				fmt.Sprintf("--%s=%s", paycli.FlagFileInput, invalidPropFile.Name()),
			},
			expectErr:    true,
			expectedCode: 0,
			respType:     &sdk.TxResponse{},
		},
	}

	for _, tc := range testCases {
		require.NoError(s.ctx.WaitForNextBlock())
		s.Run(tc.name, func() {
			cmd := paycli.CmdPayForBlob()
			out, err := clitestutil.ExecTestCLICmd(s.ctx.Context, cmd, tc.args)
			if tc.expectErr {
				require.Error(err)
				return
			}
			require.NoError(err, "test: %s\noutput: %s", tc.name, out.String())

			err = s.ctx.Codec.UnmarshalJSON(out.Bytes(), tc.respType)
			require.NoError(err, out.String(), "test: %s, output\n:", tc.name, out.String())

			txResp := tc.respType.(*sdk.TxResponse)
			require.Equal(tc.expectedCode, txResp.Code,
				"test: %s, output\n:", tc.name, out.String())

			events := txResp.Logs[0].GetEvents()
			for _, e := range events {
				if e.Type == types.EventTypePayForBlob {
					signer := e.GetAttributes()[0].GetValue()
					_, err = sdk.AccAddressFromBech32(signer)
					require.NoError(err)
					blob, err := hex.DecodeString(tc.args[1])
					require.NoError(err)
					blobSize, err := strconv.ParseInt(e.GetAttributes()[1].GetValue(), 10, 64)
					require.NoError(err)
					require.Equal(len(blob), int(blobSize))
				}
			}

			// wait for the tx to be indexed
			s.Require().NoError(s.ctx.WaitForNextBlock())

			// attempt to query for the transaction using the tx's hash
			res, err := testnode.QueryWithoutProof(s.ctx.Context, txResp.TxHash)
			require.NoError(err)
			require.Equal(abci.CodeTypeOK, res.TxResult.Code)
		})
	}
}

func TestIntegrationTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode.")
	}
	suite.Run(t, NewIntegrationTestSuite(testnode.DefaultConfig()))
}

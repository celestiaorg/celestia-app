package testutil

import (
	"encoding/hex"
	"fmt"
	"strconv"
	"testing"

	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/gogo/protobuf/proto"
	"github.com/stretchr/testify/suite"

	clitestutil "github.com/cosmos/cosmos-sdk/testutil/cli"
	cosmosnet "github.com/cosmos/cosmos-sdk/testutil/network"
	sdk "github.com/cosmos/cosmos-sdk/types"

	"github.com/celestiaorg/celestia-app/x/blob/types"

	"github.com/celestiaorg/celestia-app/testutil/network"
	"github.com/celestiaorg/celestia-app/testutil/testfactory"
	paycli "github.com/celestiaorg/celestia-app/x/blob/client/cli"
	abci "github.com/tendermint/tendermint/abci/types"
)

// username is used to create a funded genesis account under this name
const username = "test"

type IntegrationTestSuite struct {
	suite.Suite

	cfg     cosmosnet.Config
	network *cosmosnet.Network
	kr      keyring.Keyring
}

func NewIntegrationTestSuite(cfg cosmosnet.Config) *IntegrationTestSuite {
	return &IntegrationTestSuite{cfg: cfg}
}

func (s *IntegrationTestSuite) SetupSuite() {
	if testing.Short() {
		s.T().Skip("skipping integration test in short mode.")
	}
	s.T().Log("setting up integration test suite")

	net := network.New(s.T(), s.cfg, username)

	s.network = net
	s.kr = net.Validators[0].ClientCtx.Keyring
	_, err := s.network.WaitForHeight(1)
	s.Require().NoError(err)
}

func (s *IntegrationTestSuite) TearDownSuite() {
	s.T().Log("tearing down integration test suite")
	s.network.Cleanup()
}

func (s *IntegrationTestSuite) TestSubmitWirePayForBlob() {
	require := s.Require()
	val := s.network.Validators[0]

	// some hex namespace
	hexNS := "0102030405060708"
	// some hex blob
	hexBlob := "0204033704032c0b162109000908094d425837422c2116"

	testCases := []struct {
		name         string
		args         []string
		expectErr    bool
		expectedCode uint32
		respType     proto.Message
	}{
		{
			"valid transaction",
			[]string{
				hexNS,
				hexBlob,
				fmt.Sprintf("--from=%s", username),
				fmt.Sprintf("--%s=%s", flags.FlagBroadcastMode, flags.BroadcastBlock),
				fmt.Sprintf("--%s=%s", flags.FlagFees, sdk.NewCoins(sdk.NewCoin(s.cfg.BondDenom, sdk.NewInt(2))).String()),
				fmt.Sprintf("--%s=true", flags.FlagSkipConfirmation),
			},
			false, 0, &sdk.TxResponse{},
		},
	}

	for _, tc := range testCases {
		tc := tc
		s.Require().NoError(s.network.WaitForNextBlock())
		s.Run(tc.name, func() {
			cmd := paycli.CmdPayForBlob()
			clientCtx := val.ClientCtx

			out, err := clitestutil.ExecTestCLICmd(clientCtx, cmd, tc.args)
			if tc.expectErr {
				require.Error(err)
				return
			}
			require.NoError(err, "test: %s\noutput: %s", tc.name, out.String())

			err = clientCtx.Codec.UnmarshalJSON(out.Bytes(), tc.respType)
			require.NoError(err, out.String(), "test: %s, output\n:", tc.name, out.String())

			txResp := tc.respType.(*sdk.TxResponse)
			require.Equal(tc.expectedCode, txResp.Code,
				"test: %s, output\n:", tc.name, out.String())

			events := txResp.Logs[0].GetEvents()
			for _, e := range events {
				switch e.Type {
				case types.EventTypePayForBlob:
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
			s.Require().NoError(s.network.WaitForNextBlock())

			// attempt to query for the transaction using the tx's hash
			res, err := testfactory.QueryWithoutProof(clientCtx, txResp.TxHash)
			require.NoError(err)
			require.Equal(abci.CodeTypeOK, res.TxResult.Code)
		})
	}
}

func TestIntegrationTestSuite(t *testing.T) {
	suite.Run(t, NewIntegrationTestSuite(network.DefaultConfig()))
}

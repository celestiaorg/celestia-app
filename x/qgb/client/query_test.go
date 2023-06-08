package client_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/test/util/network"
	"github.com/celestiaorg/celestia-app/x/qgb/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	clitestutil "github.com/cosmos/cosmos-sdk/testutil/cli"
	cosmosnet "github.com/cosmos/cosmos-sdk/testutil/network"
	"github.com/stretchr/testify/suite"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

type CLITestSuite struct {
	suite.Suite
	cfg     cosmosnet.Config
	network *cosmosnet.Network
	kr      keyring.Keyring
}

func TestQGBCLI(t *testing.T) {
	suite.Run(t, new(CLITestSuite))
}

func (s *CLITestSuite) SetupSuite() {
	if testing.Short() {
		s.T().Skip("skipping QGB CLI tests in short mode.")
	}
	s.T().Log("setting up QGB CLI test suite")

	cfg := network.DefaultConfig()
	cfg.EnableTMLogging = false
	cfg.MinGasPrices = "0utia"
	cfg.NumValidators = 1
	cfg.TargetHeightDuration = time.Millisecond
	s.cfg = cfg

	numAccounts := 120
	accounts := make([]string, numAccounts)
	for i := 0; i < numAccounts; i++ {
		accounts[i] = tmrand.Str(20)
	}

	net := network.New(s.T(), cfg, accounts...)

	s.network = net
	s.kr = net.Validators[0].ClientCtx.Keyring
	_, err := s.network.WaitForHeight(2)
	s.Require().NoError(err)
}

func (s *CLITestSuite) TearDownSuite() {
	s.T().Log("tearing down QGB CLI test suite")
	s.network.Cleanup()
}

func (s *CLITestSuite) TestQueryAttestationByNonce() {
	_, err := s.network.WaitForHeight(402)
	s.Require().NoError(err)
	val := s.network.Validators[0]
	testCases := []struct {
		name      string
		nonce     string
		expectErr bool
	}{
		{
			name:      "query the first valset that's created on chain startup",
			nonce:     "1",
			expectErr: false,
		},
		{
			name:      "query the first data commitment",
			nonce:     "2",
			expectErr: false,
		},
		{
			name:      "negative attestation nonce",
			nonce:     "-1",
			expectErr: true,
		},
		{
			name:      "zero attestation nonce",
			nonce:     "0",
			expectErr: true,
		},
		{
			name:      "higher attestation nonce than latest attestation nonce",
			nonce:     "100",
			expectErr: true,
		},
	}

	for _, tc := range testCases {
		s.T().Run(tc.name, func(t *testing.T) {
			cmd := client.CmdQueryAttestationByNonce()
			clientCtx := val.ClientCtx

			_, err := clitestutil.ExecTestCLICmd(clientCtx, cmd, []string{tc.nonce})
			if tc.expectErr {
				s.Assert().Error(err)
			} else {
				s.Assert().NoError(err)
			}
		})
	}
}

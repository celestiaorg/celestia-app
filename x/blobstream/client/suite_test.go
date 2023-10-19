package client_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/test/util/network"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
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

func (s *CLITestSuite) SetupSuite() {
	if testing.Short() {
		s.T().Skip("skipping Blobstream CLI tests in short mode.")
	}
	s.T().Log("setting up Blobstream CLI test suite")

	cfg := network.DefaultConfig()
	cfg.EnableTMLogging = false
	cfg.MinGasPrices = "0utia"
	cfg.NumValidators = 1
	cfg.TimeoutCommit = time.Millisecond
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
	s.T().Log("tearing down Blobstream CLI test suite")
	s.network.Cleanup()
}

func TestBlobstreamCLI(t *testing.T) {
	suite.Run(t, new(CLITestSuite))
}

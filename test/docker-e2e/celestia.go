package docker_e2e

import (
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/chatton/interchaintest"
	"github.com/chatton/interchaintest/ibc"
	"github.com/chatton/interchaintest/testutil/maps"
	"github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/moby/moby/client"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest"
)

const (
	repo = "ghcr.io/celestiaorg/celestia-app-multiplexer"
)

type CelestiaTestSuite struct {
	suite.Suite
	logger  *zap.Logger
	client  *client.Client
	network string
}

func (s *CelestiaTestSuite) SetupSuite() {
	s.logger = zaptest.NewLogger(s.T())
	s.logger.Info("Setting up Celestia test suite")
	s.client, s.network = interchaintest.DockerSetup(s.T())
}

func (s *CelestiaTestSuite) CreateCelestiaChain(tag string, appVersion string) (ibc.Chain, error) {
	// Define the number of validators
	numValidators := 4
	numFullNodes := 0

	enc := testutil.MakeTestEncodingConfig(app.ModuleEncodingRegisters...)
	return interchaintest.NewChain(s.logger, s.T().Name(), s.client, s.network, &interchaintest.ChainSpec{
		Name:          "celestia",
		ChainName:     "celestia",
		Version:       tag,
		NumValidators: &numValidators,
		NumFullNodes:  &numFullNodes,
		Config: ibc.Config{
			ModifyGenesis: func(config ibc.Config, bytes []byte) ([]byte, error) {
				return maps.SetField(bytes, "consensus.params.version.app", appVersion)
			},
			EncodingConfig:      &enc,
			AdditionalStartArgs: []string{"--force-no-bbr", "--grpc.enable", "--grpc.address", "0.0.0.0:9090", "--rpc.grpc_laddr=tcp://0.0.0.0:9099"},
			Type:                "cosmos",
			ChainID:             "celestia",
			Images: []ibc.DockerImage{
				{
					Repository: repo,
					Version:    tag,
					UIDGID:     "10001:10001",
				},
			},
			Bin:           "celestia-appd",
			Bech32Prefix:  "celestia",
			Denom:         "utia",
			GasPrices:     "0.025utia",
			GasAdjustment: 1.3,
		},
	})
}

package docker_e2e

import (
	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"context"
	"fmt"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	tastoracontainertypes "github.com/celestiaorg/tastora/framework/docker/container"
	"github.com/celestiaorg/tastora/framework/testutil/query"
	"github.com/celestiaorg/tastora/framework/testutil/sdkacc"
	"github.com/cosmos/cosmos-sdk/types/module/testutil"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/tastora/framework/docker"
	"github.com/celestiaorg/tastora/framework/docker/ibc"
	"github.com/celestiaorg/tastora/framework/docker/ibc/relayer"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
	sdktx "github.com/cosmos/cosmos-sdk/client/tx"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	icacontrollertypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/controller/types"
	icatypes "github.com/cosmos/ibc-go/v8/modules/apps/27-interchain-accounts/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	clienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	"github.com/stretchr/testify/suite"
	"golang.org/x/sync/errgroup"
)

func TestIBCTestSuite(t *testing.T) {
	suite.Run(t, new(IBCTestSuite))
}

// IBCTestSuite provides common IBC test infrastructure
type IBCTestSuite struct {
	CelestiaTestSuite

	// IBC-specific fields set up in SetupTest
	chainA          *docker.Chain // Celestia
	chainB          *docker.Chain // Simapp
	hermes          *relayer.Hermes
	connection      ibc.Connection
	transferChannel ibc.Channel

	// Transaction clients
	celestiaTxClient *user.TxClient
}

// setupIBCInfrastructure sets up IBC test infrastructure with specified app version
func (s *IBCTestSuite) setupIBCInfrastructure(appVersion uint64) {
	ctx := context.Background()
	t := s.T()

	tag, err := dockerchain.GetCelestiaTagStrict()
	s.Require().NoError(err, "failed to get celestia tag")

	// setup chains in parallel
	s.celestiaCfg = dockerchain.DefaultConfig(s.client, s.network).WithTag(tag)
	s.celestiaCfg.Genesis = s.celestiaCfg.Genesis.WithAppVersion(appVersion)

	g, gCtx := errgroup.WithContext(ctx)

	g.Go(func() error {
		var err error
		s.chainA, err = dockerchain.NewCelestiaChainBuilder(t, s.celestiaCfg).Build(gCtx)
		if err != nil {
			return fmt.Errorf("failed to build chain A: %w", err)
		}
		return s.chainA.Start(gCtx)
	})

	g.Go(func() error {
		var err error
		builder := s.newSimappChainBuilder(t, s.celestiaCfg)
		s.chainB, err = builder.Build(gCtx)
		if err != nil {
			return fmt.Errorf("failed to build chain B: %w", err)
		}
		return s.chainB.Start(gCtx)
	})

	s.Require().NoError(g.Wait(), "failed to setup chains")

	// create and initialize hermes (but don't start it)
	s.hermes, err = relayer.NewHermes(ctx, s.client, t.Name(), s.network, 0, s.logger)
	s.Require().NoError(err, "failed to create hermes")

	err = s.hermes.Init(ctx, s.chainA, s.chainB)
	s.Require().NoError(err, "failed to initialize hermes")

	// create IBC clients
	err = s.hermes.CreateClients(ctx, s.chainA, s.chainB)
	s.Require().NoError(err, "failed to create IBC clients")

	// setup tx client for Celestia
	s.celestiaTxClient, err = s.setupTxClient(ctx, s.chainA)
	s.Require().NoError(err, "failed to setup tx client for celestia chain")

	// establish connection and transfer channel
	s.connection, s.transferChannel = s.establishIBCConnection(ctx)
}

// TearDownTest cleans up resources after each test
func (s *IBCTestSuite) TearDownTest() {
	ctx := context.Background()
	t := s.T()

	if s.chainA != nil {
		if err := s.chainA.Stop(ctx); err != nil {
			t.Logf("Error stopping chain A: %v", err)
		}
	}
	if s.chainB != nil {
		if err := s.chainB.Stop(ctx); err != nil {
			t.Logf("Error stopping chain B: %v", err)
		}
	}
	if s.hermes != nil {
		if err := s.hermes.Stop(ctx); err != nil {
			t.Logf("Error stopping hermes: %v", err)
		}
	}
}

// TestIBC tests IBC functionality by
// - deploying 2 chains
// - deploying hermes
// - creating an IBC connection and channel
// - performs a token transfer
// - upgrades from app version N-1 to N
// - performs token transfer over the same channel
// - creates a new connection and channel after upgrade
// - performs token transfer over a new channel
func (s *IBCTestSuite) TestIBC() {
	if testing.Short() {
		s.T().Skip("skipping IBC test in short mode")
	}

	ctx := context.Background()
	t := s.T()

	// override default setup to use base version for upgrade test
	baseAppVersion := appconsts.Version - 1
	targetAppVersion := appconsts.Version

	var channel ibc.Channel

	t.Run("setup_ibc_infrastructure", func(t *testing.T) {
		s.setupIBCInfrastructure(baseAppVersion)
	})

	t.Run("start_relayer", func(t *testing.T) {
		t.Logf("Starting hermes relayer")
		err := s.hermes.Start(ctx)
		s.Require().NoError(err, "failed to start hermes relayer")
	})

	t.Run("initial_transfers", func(t *testing.T) {
		t.Logf("Performing initial transfers")
		s.testTokenTransfers(ctx, s.transferChannel)
	})

	t.Run("upgrade_app_version", func(t *testing.T) {
		t.Logf("Upgrading app version from %d to %d", baseAppVersion, targetAppVersion)
		s.upgradeChain(ctx, s.chainA, targetAppVersion)
	})

	t.Run("existing_channel_transfers", func(t *testing.T) {
		s.testTokenTransfers(ctx, s.transferChannel)
	})

	t.Run("new_connection_and_channel", func(t *testing.T) {
		t.Logf("Creating new connection and channel after upgrade")
		_, channel = s.establishIBCConnection(ctx)
	})

	t.Run("new_channel_transfers", func(t *testing.T) {
		t.Logf("Performing transfers over new channel")
		s.testTokenTransfers(ctx, channel)
	})
}

// TestICA tests ICA functionality by
// - deploying 2 chains (Celestia as host, Simapp as controller)
// - deploying hermes relayer
// - creating an IBC connection
// - registering an interchain account
// - verifying the ICA account was created successfully
// - funding the ICA account with tokens
// - executing a bank send transaction through the ICA
func (s *IBCTestSuite) TestICA() {
	if testing.Short() {
		s.T().Skip("skipping ICA test in short mode")
	}

	ctx := context.Background()
	t := s.T()

	t.Run("setup_ibc_infrastructure", func(t *testing.T) {
		s.setupIBCInfrastructure(appconsts.Version)
	})

	t.Run("start_relayer", func(t *testing.T) {
		t.Logf("Starting hermes relayer")
		err := s.hermes.Start(ctx)
		s.Require().NoError(err, "failed to start hermes relayer")
	})

	t.Run("register_ica", func(t *testing.T) {
		t.Logf("Registering ICA via message broadcasting")
		s.registerInterchainAccount(ctx)
	})

	var icaAddress string
	t.Run("verify_ica_registration", func(t *testing.T) {
		t.Logf("Verifying ICA registration")
		icaAddress = s.verifyICARegistration(ctx)
	})

	t.Run("fund_ica_account", func(t *testing.T) {
		t.Logf("Funding the ICA account")
		s.fundICAAccount(ctx, icaAddress)
	})

	t.Run("perform_ica_bank_send", func(t *testing.T) {
		t.Logf("Performing bank send via ICA")
		s.performICABankSend(ctx, icaAddress)
	})
}

// establishIBCConnection creates IBC connection and channel between the chains
func (s *IBCTestSuite) establishIBCConnection(ctx context.Context) (ibc.Connection, ibc.Channel) {
	connection, err := s.hermes.CreateConnections(ctx, s.chainA, s.chainB)
	s.Require().NoError(err, "failed to create IBC connection")

	channelOpts := ibc.CreateChannelOptions{
		SourcePortName: "transfer",
		DestPortName:   "transfer",
		Order:          ibc.OrderUnordered,
		Version:        "ics20-1",
	}

	channel, err := s.hermes.CreateChannel(ctx, s.chainA, connection, channelOpts)
	s.Require().NoError(err, "failed to create IBC channel")

	return connection, channel
}

// testTokenTransfers tests token transfers from Celestia to simapp
func (s *IBCTestSuite) testTokenTransfers(ctx context.Context, channel ibc.Channel) {
	sourceWallet := s.chainA.GetFaucetWallet().(*docker.Wallet)
	destWallet := s.chainB.GetFaucetWallet().(*docker.Wallet)
	ibcTransfer := s.createIBCTransferMsg(sourceWallet, destWallet, channel, "utia", 100000)

	// Submit transaction and verify results - always use TxClient since we only transfer from Celestia
	s.submitTransactionAndVerify(ctx, ibcTransfer, "utia", 100000)
}

// createIBCTransferMsg creates an IBC transfer message
func (s *IBCTestSuite) createIBCTransferMsg(sourceWallet, destWallet *docker.Wallet, channel ibc.Channel, denom string, amount int64) *ibctransfertypes.MsgTransfer {
	destAddr, err := sdkacc.AddressFromWallet(destWallet)
	s.Require().NoError(err, "failed to parse destination address")

	transferAmount := sdkmath.NewInt(amount)
	return ibctransfertypes.NewMsgTransfer(
		channel.PortID,
		channel.ChannelID,
		sdk.NewCoin(denom, transferAmount),
		sourceWallet.GetFormattedAddress(),
		destAddr.String(),
		clienttypes.ZeroHeight(),
		uint64(time.Now().Add(time.Hour).UnixNano()),
		"",
	)
}

// submitTransactionAndVerify submits a transaction using the appropriate method and verifies results
func (s *IBCTestSuite) submitTransactionAndVerify(ctx context.Context, msg *ibctransfertypes.MsgTransfer, denom string, amount int64) {
	channel := ibc.Channel{PortID: msg.SourcePort, CounterpartyID: msg.SourceChannel}
	ibcDenom := s.calculateIBCDenom(channel, denom)

	destWallet := s.chainB.GetFaucetWallet().(*docker.Wallet)
	destBalance := s.getBalance(ctx, s.chainB, destWallet.GetFormattedAddress(), ibcDenom)

	// Use TxClient for Celestia chain
	resp, err := s.celestiaTxClient.SubmitTx(ctx, []sdk.Msg{msg}, user.SetGasLimit(200000), user.SetFee(5000))
	s.Require().NoError(err, "failed to submit IBC transfer via TxClient")

	s.Require().Equal(uint32(0), resp.Code, "IBC transfer tx failed with code %d", resp.Code)

	err = wait.ForBlocks(ctx, 5, s.chainA)
	s.Require().NoError(err, "failed to wait for blocks")

	transferAmount := sdkmath.NewInt(amount)
	s.verifyTransferResults(ctx, destWallet, ibcDenom, destBalance, transferAmount)
}

// verifyTransferResults checks the final balances and verifies the transfer results
func (s *IBCTestSuite) verifyTransferResults(ctx context.Context, destWallet *docker.Wallet, ibcDenom string, destBalance, transferAmount sdkmath.Int) {
	finalDestBalance := s.getBalance(ctx, s.chainB, destWallet.GetFormattedAddress(), ibcDenom)

	expectedDestBalance := destBalance.Add(transferAmount)
	s.Require().True(finalDestBalance.Equal(expectedDestBalance),
		"destination balance mismatch: expected %s, got %s", expectedDestBalance.String(), finalDestBalance.String())
}

// upgradeChain upgrades the celestia chain from baseAppVersion to targetAppVersion
// This reuses the existing upgrade logic from e2e_upgrade_test.go
func (s *IBCTestSuite) upgradeChain(ctx context.Context, chain *docker.Chain, targetAppVersion uint64) {
	validatorNode := chain.GetNodes()[0]
	cfg := s.celestiaCfg
	kr := cfg.Genesis.Keyring()

	records, err := kr.List()
	s.Require().NoError(err, "failed to list keyring records")

	upgradeHeight := s.signalAndGetUpgradeHeight(ctx, chain, validatorNode, cfg, records, targetAppVersion)

	rpcClient, err := validatorNode.GetRPCClient()
	s.Require().NoError(err, "failed to get RPC client")

	status, err := rpcClient.Status(ctx)
	s.Require().NoError(err, "failed to get node status")
	currentHeight := status.SyncInfo.LatestBlockHeight

	blocksToWait := int(upgradeHeight-currentHeight) + 2
	s.Require().NoError(wait.ForBlocks(ctx, blocksToWait, chain))

	abciInfo, err := rpcClient.ABCIInfo(ctx)
	s.Require().NoError(err, "failed to fetch ABCI info")
	s.Require().Equal(targetAppVersion, abciInfo.Response.GetAppVersion(), "app version mismatch after upgrade")

	// recreate tx client after upgrade to refresh account sequence
	s.celestiaTxClient, err = s.setupTxClient(ctx, chain)
	s.Require().NoError(err, "failed to setup tx client after upgrade")
}

// setupTxClient sets up a tx client using the node's keyring
func (s *IBCTestSuite) setupTxClient(ctx context.Context, chain *docker.Chain) (*user.TxClient, error) {
	node := chain.GetNodes()[0].(*docker.ChainNode)

	keyring, err := node.GetKeyring()
	if err != nil {
		return nil, err
	}

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	return user.SetupTxClient(
		ctx,
		keyring,
		node.GrpcConn,
		encCfg,
	)
}

// getBalance gets the balance of a specific denom for an address
func (s *IBCTestSuite) getBalance(ctx context.Context, chain *docker.Chain, address, denom string) sdkmath.Int {
	node := chain.GetNode()
	if node.GrpcConn == nil {
		s.T().Logf("GRPC connection is nil for chain %s, returning zero balance", chain.GetChainID())
		return sdkmath.ZeroInt()
	}

	amount, err := query.Balance(ctx, node.GrpcConn, address, denom)
	s.Require().NoError(err, "failed to query balance for %s %s", address, denom)
	return amount
}

// calculateIBCDenom calculates the IBC denomination using ibc-go utilities
func (s *IBCTestSuite) calculateIBCDenom(channel ibc.Channel, baseDenom string) string {
	prefixedDenom := ibctransfertypes.GetPrefixedDenom(
		channel.PortID,
		channel.CounterpartyID,
		baseDenom,
	)
	return ibctransfertypes.ParseDenomTrace(prefixedDenom).IBCDenom()
}

// newSimappChainBuilder builds a standard simapp chain without token filters
func (s *IBCTestSuite) newSimappChainBuilder(t *testing.T, cfg *dockerchain.Config) *docker.ChainBuilder {
	encodingConfig := testutil.MakeTestEncodingConfig(app.ModuleEncodingRegisters...)
	return docker.NewChainBuilder(t).
		WithEncodingConfig(&encodingConfig).
		WithName("simapp").
		WithChainID("chain-b").
		// TODO: this is a custom built simapp that has the bech32prefix as "celestia" as a workaround for the global
		// SDK config not being usable when 2 chains have a different bech32 prefix (e.g. "celestia" and "cosmos" ) because it is sealed.
		WithImage(tastoracontainertypes.NewImage("ghcr.io/chatton/ibc-go-simd", "v8.5.0", "1000:1000")).
		WithBinaryName("simd").
		WithBech32Prefix("celestia").
		WithDenom("utia").
		WithGasPrices("0.000001utia").
		WithDockerNetworkID(cfg.DockerNetworkID).
		WithDockerClient(cfg.DockerClient).
		WithChainID("chain-b").
		WithNode(docker.NewChainNodeConfigBuilder().Build())
}

// registerInterchainAccount registers an ICA using message broadcasting instead of CLI
func (s *IBCTestSuite) registerInterchainAccount(ctx context.Context) {
	controllerWallet := s.chainB.GetFaucetWallet()

	msg := icacontrollertypes.NewMsgRegisterInterchainAccount(
		s.connection.ConnectionID,
		controllerWallet.GetFormattedAddress(),
		"",
	)

	// Use Broadcaster for simapp (chainB)
	broadcaster := docker.NewBroadcaster(s.chainB)
	broadcaster.ConfigureFactoryOptions(func(factory sdktx.Factory) sdktx.Factory {
		return factory.WithGas(400000) // set higher gas limit for ICA registration
	})

	txResponse, err := broadcaster.BroadcastMessages(ctx, controllerWallet, msg)
	s.Require().NoError(err, "failed to broadcast ICA registration message")
	s.Require().NotEmpty(txResponse.TxHash, "transaction hash should not be empty")
	s.Require().Equal(uint32(0), txResponse.Code, "ICA registration tx failed with code %d: %s", txResponse.Code, txResponse.RawLog)

	s.T().Logf("ICA registration successful! TxHash: %s", txResponse.TxHash)

	// wait longer for IBC packets to be processed
	err = wait.ForBlocks(ctx, 10, s.chainB)
	s.Require().NoError(err, "failed to wait for blocks after ICA registration")
}

// verifyICARegistration verifies the ICA account was created and channels are established
func (s *IBCTestSuite) verifyICARegistration(ctx context.Context) string {
	controllerWallet := s.chainB.GetFaucetWallet()

	// query the ICA account address using gRPC
	controllerNode := s.chainB.GetNodes()[0].(*docker.ChainNode)
	s.Require().NotNil(controllerNode.GrpcConn, "controller gRPC connection is nil")

	s.T().Logf("Querying ICA for owner: %s, connection: %s", controllerWallet.GetFormattedAddress(), s.connection.ConnectionID)

	queryClient := icacontrollertypes.NewQueryClient(controllerNode.GrpcConn)

	// wait a bit more and retry the query
	err := wait.ForBlocks(ctx, 5, s.chainB, s.chainA)
	s.Require().NoError(err, "failed to wait for additional blocks")

	resp, err := queryClient.InterchainAccount(ctx, &icacontrollertypes.QueryInterchainAccountRequest{
		Owner:        controllerWallet.GetFormattedAddress(),
		ConnectionId: s.connection.ConnectionID,
	})
	s.Require().NoError(err, "failed to query ICA account")
	s.Require().NotEmpty(resp.Address, "ICA account address should not be empty")

	s.T().Logf("ICA account address: %s", resp.Address)
	return resp.Address
}

// fundICAAccount sends tokens to the ICA account so it can perform transactions
func (s *IBCTestSuite) fundICAAccount(ctx context.Context, icaAddress string) {
	hostWallet := s.chainA.GetFaucetWallet()
	fundAmount := sdkmath.NewInt(10000000) // 10 utia

	fundMsg := &banktypes.MsgSend{
		FromAddress: hostWallet.GetFormattedAddress(),
		ToAddress:   icaAddress,
		Amount:      sdk.NewCoins(sdk.NewCoin("utia", fundAmount)),
	}

	// Use TxClient for Celestia (chainA)
	resp, err := s.celestiaTxClient.SubmitTx(ctx, []sdk.Msg{fundMsg}, user.SetGasLimit(200000), user.SetFee(5000))
	s.Require().NoError(err, "failed to fund ICA account")
	s.Require().Equal(uint32(0), resp.Code, "funding failed with code %d", resp.Code)

	s.T().Logf("ICA funded with %s utia. TxHash: %s", fundAmount.String(), resp.TxHash)
}

// performICABankSend performs a bank send transaction via the ICA
func (s *IBCTestSuite) performICABankSend(ctx context.Context, icaAddress string) {
	controllerWallet := s.chainB.GetFaucetWallet()
	recipientWallet := s.chainA.GetFaucetWallet()
	sendAmount := sdkmath.NewInt(1000000) // 1 utia

	// create bank send message to be executed by the ICA
	bankSendMsg := &banktypes.MsgSend{
		FromAddress: icaAddress,
		ToAddress:   recipientWallet.GetFormattedAddress(),
		Amount:      sdk.NewCoins(sdk.NewCoin("utia", sendAmount)),
	}

	initialBalance := s.getBalance(ctx, s.chainA, recipientWallet.GetFormattedAddress(), "utia")

	// convert message to Any type and create CosmosTx properly
	msgAny, err := codectypes.NewAnyWithValue(bankSendMsg)
	s.Require().NoError(err, "failed to create Any from bank send message")

	// create CosmosTx
	cosmosTx := &icatypes.CosmosTx{
		Messages: []*codectypes.Any{msgAny},
	}

	// encode using the app's encoding config
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	txBytes, err := encCfg.Codec.Marshal(cosmosTx)
	s.Require().NoError(err, "failed to marshal CosmosTx")

	// create packet data
	packetData := icatypes.InterchainAccountPacketData{
		Type: icatypes.EXECUTE_TX,
		Data: txBytes,
		Memo: "",
	}

	// create the correct ICA message
	icaMsg := icacontrollertypes.NewMsgSendTx(
		controllerWallet.GetFormattedAddress(),
		s.connection.ConnectionID,
		uint64(time.Hour.Nanoseconds()),
		packetData,
	)

	// Use Broadcaster for simapp (chainB)
	broadcaster := docker.NewBroadcaster(s.chainB)
	broadcaster.ConfigureFactoryOptions(func(factory sdktx.Factory) sdktx.Factory {
		return factory.WithGas(500000)
	})

	txResponse, err := broadcaster.BroadcastMessages(ctx, controllerWallet, icaMsg)
	s.Require().NoError(err, "failed to broadcast ICA bank send")
	s.Require().Equal(uint32(0), txResponse.Code, "ICA send failed: %s", txResponse.RawLog)

	s.T().Logf("ICA bank send successful! TxHash: %s", txResponse.TxHash)

	// wait for IBC packet processing
	err = wait.ForBlocks(ctx, 10, s.chainB, s.chainA)
	s.Require().NoError(err, "failed to wait for blocks")

	// verify the send was executed (account for gas fees)
	finalBalance := s.getBalance(ctx, s.chainA, recipientWallet.GetFormattedAddress(), "utia")
	s.Require().True(finalBalance.GT(initialBalance),
		"ICA bank send failed: balance should have increased from %s to %s", initialBalance.String(), finalBalance.String())
	s.T().Logf("Bank send via ICA successful: balance increased from %s to %s utia", initialBalance.String(), finalBalance.String())
}

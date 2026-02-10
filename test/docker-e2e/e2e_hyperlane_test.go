package docker_e2e

import (
	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"context"
	"encoding/hex"
	"fmt"
	"math/big"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	hyputil "github.com/bcp-innovations/hyperlane-cosmos/util"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	forwardingtypes "github.com/celestiaorg/celestia-app/v7/x/forwarding/types"
	zkismtypes "github.com/celestiaorg/celestia-app/v7/x/zkism/types"
	tastoradockertypes "github.com/celestiaorg/tastora/framework/docker"
	"github.com/celestiaorg/tastora/framework/docker/container"
	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	"github.com/celestiaorg/tastora/framework/docker/dataavailability"
	"github.com/celestiaorg/tastora/framework/docker/evstack/evmsingle"
	"github.com/celestiaorg/tastora/framework/docker/evstack/reth"
	"github.com/celestiaorg/tastora/framework/docker/hyperlane"
	"github.com/celestiaorg/tastora/framework/testutil/evm"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/gogoproto/proto"
	"github.com/ethereum/go-ethereum/accounts/abi"
	ethcommon "github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"go.uber.org/zap/zaptest"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

var (
	// NOTE: This a workaround as using the chain name "celestia" causes configuration overlay issues
	// with the Hyperlane agents container. This can be reverted when the following issue is addressed.
	// See https://github.com/hyperlane-xyz/hyperlane-monorepo/issues/7598.
	HypCelestiaChainName = "celestiadev"

	RethChainName0 = "reth0"
	RethChainName1 = "reth1"

	RethChainID0 = 1234
	RethChainID1 = 1235
)

// EvolveEVMChain encapsulates both the evolve evm sequencer node and execution client node.
type EvolveEVMChain struct {
	*evmsingle.Chain
	*reth.Node
}

func TestHyperlaneTestSuite(t *testing.T) {
	suite.Run(t, new(HyperlaneTestSuite))
}

type HyperlaneTestSuite struct {
	CelestiaTestSuite
}

func (s *HyperlaneTestSuite) SetupSuite() {
	s.logger = zaptest.NewLogger(s.T())
	s.logger.Info("Setting up Celestia test suite: " + s.T().Name())
	s.client, s.network = tastoradockertypes.Setup(s.T())

	tag, err := dockerchain.GetCelestiaTagStrict()
	s.Require().NoError(err)

	s.celestiaCfg = dockerchain.DefaultConfig(s.client, s.network).WithTag(tag)
}

func (s *HyperlaneTestSuite) TestHyperlaneTokenTransfer() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping hyperlane forwarding test in short mode")
	}

	ctx := context.Background()
	chain, err := dockerchain.NewCelestiaChainBuilder(s.T(), s.celestiaCfg).Build(ctx)
	s.Require().NoError(err)

	s.T().Cleanup(func() {
		if err := chain.Remove(ctx); err != nil {
			s.T().Logf("Error removing chain: %v", err)
		}
	})

	err = chain.Start(ctx)
	s.Require().NoError(err)

	da := s.WithBridgeNodeNetwork(ctx, chain)

	reth := s.BuildEvolveEVMChain(ctx, s.BridgeNodeAddress(da), RethChainName0, RethChainID0)

	hypConfig := hyperlane.Config{
		Logger:          s.logger,
		DockerClient:    s.client,
		DockerNetworkID: s.network,
		HyperlaneImage:  hyperlane.DefaultDeployerImage(),
	}

	hypChainProvider := []hyperlane.ChainConfigProvider{reth, chain}
	hyp, err := hyperlane.NewDeployer(ctx, hypConfig, t.Name(), hypChainProvider)
	s.Require().NoError(err)

	s.Require().NoError(hyp.Deploy(ctx))

	broadcaster := cosmos.NewBroadcaster(chain)
	faucet := chain.GetFaucetWallet()

	config, err := hyp.DeployCosmosNoopISM(ctx, broadcaster, faucet)
	s.Require().NoError(err)
	s.Require().NotNil(t, config)

	tokenRouter, err := hyp.GetEVMWarpTokenAddress()
	s.Require().NoError(err)

	// Register Hyperlane token router connections between celestia and both evm chains
	s.EnrollRemoteRouters(ctx, chain, reth, hyp, tokenRouter, config.TokenID)

	// Create and fund a new test wallet via the chain faucet
	wallet, err := chain.CreateWallet(ctx, "test-hyperlane")
	s.Require().NoError(err)

	coin := sdk.NewCoin(chain.Config.Denom, sdkmath.NewInt(1000))
	msgBankSend := banktypes.NewMsgSend(faucet.Address, wallet.Address, sdk.NewCoins(coin))

	txResp, err := broadcaster.BroadcastMessages(ctx, faucet, msgBankSend)
	s.Require().NoError(err)
	s.Require().Equal(uint32(0), txResp.Code, "tx failed: code=%d, log=%s", txResp.Code, txResp.RawLog)

	s.StartRelayerAgent(ctx, hyp)

	// Initial transfer of utia collateral token to reth evm chain
	rethDomain := s.GetDomainForChain(ctx, reth.HyperlaneChainName(), hyp)
	rethRecipient := ethcommon.HexToAddress("0xaF9053bB6c4346381C77C2FeD279B17ABAfCDf4d")

	s.SendTransferRemoteTx(ctx, chain, config.TokenID, rethDomain, rethRecipient, coin.Amount)

	s.AssertERC20Balance(ctx, reth, tokenRouter, rethRecipient, coin.Amount.BigInt())

	balance := s.QueryBankBalance(ctx, chain, wallet.FormattedAddress, chain.Config.Denom)

	// Execute the hyperlane warp transfer from reth to celestia
	amount := sdkmath.NewInt(500)

	celestiaDomain := s.GetDomainForChain(ctx, HypCelestiaChainName, hyp)
	celestiaRecipient, err := bech32ToBytes(wallet.FormattedAddress)
	s.Require().NoError(err)

	s.SendTransferRemoteTxEvm(ctx, reth, tokenRouter, celestiaDomain, celestiaRecipient, amount)

	expBalance := balance.Add(amount)
	s.AssertBankBalance(ctx, chain, wallet.FormattedAddress, chain.Config.Denom, expBalance)
}

func (s *HyperlaneTestSuite) TestHyperlaneForwarding() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping hyperlane forwarding test in short mode")
	}

	ctx := context.Background()
	chain, err := dockerchain.NewCelestiaChainBuilder(s.T(), s.celestiaCfg).Build(ctx)
	s.Require().NoError(err)

	s.T().Cleanup(func() {
		if err := chain.Remove(ctx); err != nil {
			s.T().Logf("Error removing chain: %v", err)
		}
	})

	err = chain.Start(ctx)
	s.Require().NoError(err)

	da := s.WithBridgeNodeNetwork(ctx, chain)

	reth0 := s.BuildEvolveEVMChain(ctx, s.BridgeNodeAddress(da), RethChainName0, RethChainID0)
	reth1 := s.BuildEvolveEVMChain(ctx, s.BridgeNodeAddress(da), RethChainName1, RethChainID1)

	hypConfig := hyperlane.Config{
		Logger:          s.logger,
		DockerClient:    s.client,
		DockerNetworkID: s.network,
		HyperlaneImage:  hyperlane.DefaultDeployerImage(),
	}

	hypChainProvider := []hyperlane.ChainConfigProvider{reth0, reth1, chain}
	hyp, err := hyperlane.NewDeployer(ctx, hypConfig, t.Name(), hypChainProvider)
	s.Require().NoError(err)

	s.Require().NoError(hyp.Deploy(ctx))

	broadcaster := cosmos.NewBroadcaster(chain)
	faucet := chain.GetFaucetWallet()

	config, err := hyp.DeployCosmosNoopISM(ctx, broadcaster, faucet)
	s.Require().NoError(err)
	s.Require().NotNil(config)

	tokenRouter, err := hyp.GetEVMWarpTokenAddress()
	s.Require().NoError(err)

	// Register Hyperlane token router connections between celestia and both evm chains
	s.EnrollRemoteRouters(ctx, chain, reth0, hyp, tokenRouter, config.TokenID)
	s.EnrollRemoteRouters(ctx, chain, reth1, hyp, tokenRouter, config.TokenID)

	s.StartRelayerAgent(ctx, hyp)

	// Make an initial deposit of utia from celestia to reth0 chain
	initialDeposit := sdkmath.NewInt(1000)
	recipient := ethcommon.HexToAddress("0xaF9053bB6c4346381C77C2FeD279B17ABAfCDf4d")

	domain := s.GetDomainForChain(ctx, reth0.HyperlaneChainName(), hyp)
	s.SendTransferRemoteTx(ctx, chain, config.TokenID, domain, recipient, initialDeposit)

	s.AssertERC20Balance(ctx, reth0, tokenRouter, recipient, initialDeposit.BigInt())

	// Compute the forwarding address on celestia for recipient on reth1 destintation chain
	destDomain := s.GetDomainForChain(ctx, reth1.HyperlaneChainName(), hyp)
	destRecipient := "0x0000000000000000000000004A60C46F671A3B86D78E9C0B793235C2D502D44E"
	forwardAddress := s.QueryForwardingAddress(ctx, chain, destDomain, destRecipient)

	forwardAddrBytes32, err := bech32ToBytes(forwardAddress)
	s.Require().NoError(err)

	beforeForwardBalance := s.QueryBankBalance(ctx, chain, forwardAddress, chain.Config.Denom)

	// Execute the hyperlane erc20 transfer from reth0 to reth1 via celestia x/forwarding
	amount := sdkmath.NewInt(500)
	celestiaDomain := s.GetDomainForChain(ctx, HypCelestiaChainName, hyp)
	s.SendTransferRemoteTxEvm(ctx, reth0, tokenRouter, celestiaDomain, forwardAddrBytes32, amount)

	expForwardBalance := beforeForwardBalance.Add(amount)
	s.AssertBankBalance(ctx, chain, forwardAddress, chain.Config.Denom, expForwardBalance)

	destRecipientAddress := ethcommon.HexToAddress(destRecipient)
	balanceBefore := s.QueryERC20Balance(ctx, reth1, tokenRouter, destRecipientAddress)

	// Permissionless invocation of MsgForward (to be done by external relayer service)
	forwardFee := s.QueryForwardingFee(ctx, chain, destDomain)
	s.SendForwardingTx(ctx, chain, forwardAddress, destDomain, destRecipient, forwardFee)

	expectedBalance := new(big.Int).Add(balanceBefore, amount.BigInt())
	s.AssertERC20Balance(ctx, reth1, tokenRouter, destRecipientAddress, expectedBalance)
}

func (s *HyperlaneTestSuite) TestHyperlaneZKIsmStateTransition() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping hyperlane zkism state transition test in short mode")
	}

	ctx := context.Background()
	chain, err := dockerchain.NewCelestiaChainBuilder(s.T(), s.celestiaCfg).Build(ctx)
	s.Require().NoError(err)

	s.T().Cleanup(func() {
		if err := chain.Remove(ctx); err != nil {
			s.T().Logf("Error removing chain: %v", err)
		}
	})

	err = chain.Start(ctx)
	s.Require().NoError(err)

	trustedState := readTrustedState(t, "state_transition/trusted_state")

	ismID := s.CreateZKIsmWithTrustedState(ctx, chain, trustedState)
	s.Require().False(ismID.IsZeroAddress())

	broadcaster := cosmos.NewBroadcaster(chain)
	signer := chain.GetFaucetWallet()

	proofBz, pubValues := readStateTransitionProofData(s.T())
	txMsg := zkismtypes.MsgUpdateInterchainSecurityModule{
		Id:           ismID,
		Proof:        proofBz,
		PublicValues: pubValues,
		Signer:       signer.GetFormattedAddress(),
	}

	var stateTransitionVlaues zkismtypes.StateTransitionValues
	err = stateTransitionVlaues.Unmarshal(pubValues)
	s.Require().NoError(err)

	expStateHex := "0x" + hex.EncodeToString(stateTransitionVlaues.NewState)

	txResp, err := broadcaster.BroadcastMessages(ctx, signer, &txMsg)
	s.Require().NoError(err)
	s.Require().Equalf(uint32(0), txResp.Code, "tx failed: code=%d, log=%s", txResp.Code, txResp.RawLog)

	ismEvent := s.ParseUpdateISMEvent(txResp)
	s.Require().NotNil(ismEvent)
	s.Require().Equal(expStateHex, ismEvent.State)
}

func (s *HyperlaneTestSuite) TestHyperlaneZKIsmStateMembership() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping hyperlane zkism state membership test in short mode")
	}

	ctx := context.Background()
	chain, err := dockerchain.NewCelestiaChainBuilder(s.T(), s.celestiaCfg).Build(ctx)
	s.Require().NoError(err)

	s.T().Cleanup(func() {
		if err := chain.Remove(ctx); err != nil {
			s.T().Logf("Error removing chain: %v", err)
		}
	})

	err = chain.Start(ctx)
	s.Require().NoError(err)

	trustedState := readTrustedState(t, "state_membership/trusted_state")

	ismID := s.CreateZKIsmWithTrustedState(ctx, chain, trustedState)
	s.Require().False(ismID.IsZeroAddress())

	broadcaster := cosmos.NewBroadcaster(chain)
	signer := chain.GetFaucetWallet()

	proofBz, pubValues := readStateMembershipProofData(s.T())
	txMsg := zkismtypes.MsgSubmitMessages{
		Id:           ismID,
		Proof:        proofBz,
		PublicValues: pubValues,
		Signer:       signer.GetFormattedAddress(),
	}

	var stateMembershipValues zkismtypes.StateMembershipValues
	err = stateMembershipValues.Unmarshal(pubValues)
	s.Require().NoError(err)

	expMsgs := make([]string, 0, len(stateMembershipValues.MessageIds))
	for _, msg := range stateMembershipValues.MessageIds {
		expMsgs = append(expMsgs, "0x"+hex.EncodeToString(msg[:]))
	}

	txResp, err := broadcaster.BroadcastMessages(ctx, signer, &txMsg)
	s.Require().NoError(err)
	s.Require().Equalf(uint32(0), txResp.Code, "tx failed: code=%d, log=%s", txResp.Code, txResp.RawLog)

	msgs := s.QueryZKIsmMessages(ctx, chain, ismID.String())
	s.Require().Equal(expMsgs, msgs)
}

func (s *HyperlaneTestSuite) StartRelayerAgent(ctx context.Context, deployer *hyperlane.Deployer) {
	s.T().Helper()

	cfg := hyperlane.Config{
		Logger:          s.logger,
		DockerClient:    s.client,
		DockerNetworkID: s.network,
		HyperlaneImage:  container.NewImage("gcr.io/abacus-labs-dev/hyperlane-agent", "agents-v1.7.0", "1000:1000"),
	}

	agent, err := hyperlane.NewAgent(ctx, cfg, s.T().Name(), hyperlane.AgentTypeRelayer, deployer)
	s.Require().NoError(err)

	err = agent.Start(ctx)
	s.Require().NoError(err)

	s.T().Cleanup(func() {
		_ = agent.Stop(ctx)
		_ = agent.Remove(ctx)
	})
}

func (s *HyperlaneTestSuite) BridgeNodeAddress(da *dataavailability.Network) string {
	s.T().Helper()

	networkInfo, err := da.GetBridgeNodes()[0].GetNetworkInfo(s.T().Context())
	require.NoError(s.T(), err)

	return fmt.Sprintf("http://%s:%s", networkInfo.Internal.IP, networkInfo.Internal.Ports.RPC)
}

func (s *HyperlaneTestSuite) BuildEvolveEVMChain(ctx context.Context, daAddress, chainName string, chainID int) *EvolveEVMChain {
	s.T().Helper()

	rethNode, err := reth.NewNodeBuilderWithTestName(s.T(), s.T().Name()).
		WithName(chainName).
		WithDockerClient(s.client).
		WithDockerNetworkID(s.network).
		WithGenesis([]byte(reth.DefaultEvolveGenesisJSON(reth.WithChainID(chainID)))).
		WithHyperlaneChainName(chainName).
		WithHyperlaneChainID(uint64(chainID)).
		WithHyperlaneDomainID(uint32(chainID)).
		Build(ctx)
	s.Require().NoError(err)

	err = rethNode.Start(ctx)
	s.Require().NoError(err)

	networkInfo, err := rethNode.GetNetworkInfo(ctx)
	s.Require().NoError(err)

	genesisHash, err := rethNode.GenesisHash(ctx)
	s.Require().NoError(err)

	evmEthURL := fmt.Sprintf("http://%s:%s", networkInfo.Internal.Hostname, networkInfo.Internal.Ports.RPC)
	evmEngineURL := fmt.Sprintf("http://%s:%s", networkInfo.Internal.Hostname, networkInfo.Internal.Ports.Engine)

	seqCfg := evmsingle.NewNodeConfigBuilder().
		WithEVMEngineURL(evmEngineURL).
		WithEVMETHURL(evmEthURL).
		WithEVMJWTSecret(rethNode.JWTSecretHex()).
		WithEVMSignerPassphrase("secret").
		WithEVMBlockTime("1s").
		WithEVMGenesisHash(genesisHash).
		WithDAAddress(daAddress).
		Build()

	seqNode, err := evmsingle.NewChainBuilderWithTestName(s.T(), s.T().Name()).
		WithName(chainName).
		WithDockerClient(s.client).
		WithDockerNetworkID(s.network).
		WithNodes(seqCfg).
		Build(ctx)
	s.Require().NoError(err)

	err = seqNode.Start(ctx)
	s.Require().NoError(err)

	evmNodes := seqNode.Nodes()
	s.Require().Len(evmNodes, 1)

	waitForReady(s.T(), ctx, evmNodes[0])
	s.T().Cleanup(func() {
		_ = rethNode.Stop(ctx)
		_ = rethNode.Remove(ctx)
		_ = seqNode.Stop(ctx)
		_ = seqNode.Remove(ctx)
	})

	return &EvolveEVMChain{seqNode, rethNode}
}

func (s *HyperlaneTestSuite) EnrollRemoteRouters(ctx context.Context, chain *cosmos.Chain, evolve *EvolveEVMChain, deployer *hyperlane.Deployer, tokenRouter ethcommon.Address, tokenID hyputil.HexAddress) {
	s.T().Helper()

	networkInfo, err := evolve.GetNetworkInfo(ctx)
	s.Require().NoError(err)

	rpcURL := fmt.Sprintf("http://%s", networkInfo.External.RPCAddress())

	chainName := evolve.HyperlaneChainName()
	celestiaDomain := s.GetDomainForChain(ctx, HypCelestiaChainName, deployer)

	txHash, err := deployer.EnrollRemoteRouter(ctx, tokenRouter.Hex(), celestiaDomain, tokenID.String(), chainName, rpcURL)
	s.Require().NoError(err)
	s.Require().NotEmpty(txHash, "tx hash should be non-empty")

	broadcaster := cosmos.NewBroadcaster(chain)
	signer := chain.GetFaucetWallet()

	evmDomain := s.GetDomainForChain(ctx, chainName, deployer)
	remoteTokenRouter := evm.PadAddress(tokenRouter) // leftpad to bytes32
	err = deployer.EnrollRemoteRouterOnCosmos(ctx, broadcaster, signer, tokenID, evmDomain, remoteTokenRouter.String())
	s.Require().NoError(err)
}

func (s *HyperlaneTestSuite) GetDomainForChain(ctx context.Context, chainName string, deployer *hyperlane.Deployer) uint32 {
	s.T().Helper()

	schema, err := deployer.GetOnDiskSchema(ctx)
	s.Require().NoError(err)

	registryEntry, ok := schema.Registry.Chains[chainName]
	s.Require().True(ok)

	return registryEntry.Metadata.DomainID
}

func (s *HyperlaneTestSuite) AssertBankBalance(ctx context.Context, chain *cosmos.Chain, address string, denom string, expected sdkmath.Int) {
	s.T().Helper()

	s.Require().Eventually(func() bool {
		balance := s.QueryBankBalance(ctx, chain, address, denom)
		return balance.Equal(expected)
	}, time.Minute, 5*time.Second, "unexpected bank balance, expected: ", expected)
}

func (s *HyperlaneTestSuite) AssertERC20Balance(ctx context.Context, chain *EvolveEVMChain, erc20Address ethcommon.Address, account ethcommon.Address, expected *big.Int) {
	s.T().Helper()

	s.Require().Eventually(func() bool {
		balance := s.QueryERC20Balance(ctx, chain, erc20Address, account)
		return balance.Cmp(expected) == 0
	}, time.Minute, 5*time.Second, "unexpected erc20 balance, expected: ", expected)
}

func (s *HyperlaneTestSuite) QueryForwardingAddress(ctx context.Context, chain *cosmos.Chain, domain uint32, recipient string) string {
	s.T().Helper()

	networkInfo, err := chain.GetNetworkInfo(ctx)
	s.Require().NoError(err)

	grpcAddress := networkInfo.External.GRPCAddress()
	grpcConn, err := grpc.NewClient(grpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	s.Require().NoError(err)

	defer grpcConn.Close()

	req := forwardingtypes.QueryDeriveForwardingAddressRequest{
		DestDomain:    domain,
		DestRecipient: recipient,
	}

	client := forwardingtypes.NewQueryClient(grpcConn)
	resp, err := client.DeriveForwardingAddress(ctx, &req)
	s.Require().NoError(err)

	return resp.Address
}

func (s *HyperlaneTestSuite) QueryForwardingFee(ctx context.Context, chain *cosmos.Chain, destDomain uint32) sdk.Coin {
	s.T().Helper()

	networkInfo, err := chain.GetNetworkInfo(ctx)
	s.Require().NoError(err)

	grpcAddress := networkInfo.External.GRPCAddress()
	grpcConn, err := grpc.NewClient(grpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	s.Require().NoError(err)

	defer grpcConn.Close()

	req := &forwardingtypes.QueryQuoteForwardingFeeRequest{
		DestDomain: destDomain,
	}

	client := forwardingtypes.NewQueryClient(grpcConn)
	resp, err := client.QuoteForwardingFee(ctx, req)
	s.Require().NoError(err)

	return resp.Fee
}

func (s *HyperlaneTestSuite) QueryBankBalance(ctx context.Context, chain *cosmos.Chain, address, denom string) *sdkmath.Int {
	s.T().Helper()

	networkInfo, err := chain.GetNetworkInfo(ctx)
	s.Require().NoError(err)

	grpcAddress := networkInfo.External.GRPCAddress()
	grpcConn, err := grpc.NewClient(grpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	s.Require().NoError(err)

	defer grpcConn.Close()

	req := &banktypes.QueryBalanceRequest{
		Address: address,
		Denom:   denom,
	}

	client := banktypes.NewQueryClient(grpcConn)
	resp, err := client.Balance(ctx, req)
	s.Require().NoError(err)

	return &resp.Balance.Amount
}

func (s *HyperlaneTestSuite) QueryERC20Balance(ctx context.Context, chain *EvolveEVMChain, erc20Address ethcommon.Address, account ethcommon.Address) *big.Int {
	s.T().Helper()

	client, err := chain.GetEthClient(ctx)
	s.Require().NoError(err)

	balance, err := evm.GetERC20Balance(ctx, client, erc20Address, account)
	s.Require().NoError(err)

	return balance
}

func (s *HyperlaneTestSuite) SendForwardingTx(ctx context.Context, chain *cosmos.Chain, forwardAddr string, destDomain uint32, destRecipient string, maxIgpFee sdk.Coin) {
	s.T().Helper()

	broadcaster := cosmos.NewBroadcaster(chain)
	signer := chain.GetFaucetWallet()

	msgForward := forwardingtypes.NewMsgForward(
		signer.GetFormattedAddress(),
		forwardAddr,
		destDomain,
		destRecipient,
		maxIgpFee,
	)

	resp, err := broadcaster.BroadcastMessages(ctx, signer, msgForward)
	s.Require().NoError(err)
	s.Require().Equalf(uint32(0), resp.Code, "tx failed: code=%d, log=%s", resp.Code, resp.RawLog)
}

func (s *HyperlaneTestSuite) SendTransferRemoteTx(ctx context.Context, chain *cosmos.Chain, tokenID hyputil.HexAddress, domain uint32, recipient ethcommon.Address, amount sdkmath.Int) {
	s.T().Helper()

	broadcaster := cosmos.NewBroadcaster(chain)
	signer := chain.GetFaucetWallet()

	msgRemoteTransfer := &warptypes.MsgRemoteTransfer{
		Sender:            signer.GetFormattedAddress(),
		TokenId:           tokenID,
		DestinationDomain: domain,
		Recipient:         evm.PadAddress(recipient),
		Amount:            amount,
	}

	resp, err := broadcaster.BroadcastMessages(ctx, signer, msgRemoteTransfer)
	s.Require().NoError(err)
	s.Require().Equalf(uint32(0), resp.Code, "tx failed: code=%d, log=%s", resp.Code, resp.RawLog)
}

func (s *HyperlaneTestSuite) SendTransferRemoteTxEvm(ctx context.Context, chain *EvolveEVMChain, tokenRouter ethcommon.Address, domain uint32, recipient [32]byte, amount sdkmath.Int) {
	s.T().Helper()

	// TODO: should provide a signer arg rather than relying on Hyperlane config defaults
	cfg, err := chain.GetHyperlaneRelayerChainConfig(ctx)
	s.Require().NoError(err)
	s.Require().NotNil(cfg.Signer)

	networkInfo, err := chain.GetNetworkInfo(ctx)
	s.Require().NoError(err)

	sender, err := evm.NewSender(ctx, fmt.Sprintf("http://%s", networkInfo.External.RPCAddress()))
	s.Require().NoError(err)

	defer sender.Close()

	transferABI, err := readTransferRemoteABI()
	s.Require().NoError(err)

	txHash, err := sender.SendFunctionTx(ctx,
		cfg.Signer.Key,
		tokenRouter.Hex(),
		transferABI,
		"transferRemote",
		domain,
		recipient,
		amount.BigInt(),
	)
	s.Require().NoError(err)
	s.Require().NotEmpty(txHash, "tx hash should be non-empty")
}

func (s *HyperlaneTestSuite) CreateZKIsmWithTrustedState(ctx context.Context, chain *cosmos.Chain, trustedState []byte) hyputil.HexAddress {
	s.T().Helper()

	var (
		stateVkeyHash     = "0x0017bc91d53b93c46eb842d7f9020a94ea13d8877a21608b34b71fcc4da64f29"
		messageVkeyHash   = "0x004959d5fb2c3d5bc1f98e032188dd94fbb5c6b6152df356c7c20be23be824a2"
		merkleTreeAddress = "fcb1d485ef46344029d9e8a7925925e146b3430e000000000000000000000000"
	)

	groth16Vkey := readGroth16Vkey(s.T())

	stateVkeyHex := strings.TrimPrefix(stateVkeyHash, "0x")
	stateVkey, err := hex.DecodeString(stateVkeyHex)
	s.Require().NoError(err)

	messageVkeyHex := strings.TrimPrefix(messageVkeyHash, "0x")
	messageVkey, err := hex.DecodeString(messageVkeyHex)
	s.Require().NoError(err)

	merkleTreeAddr, err := hex.DecodeString(merkleTreeAddress)
	s.Require().NoError(err)

	signer := chain.GetFaucetWallet()
	txMsg := zkismtypes.MsgCreateInterchainSecurityModule{
		Creator:             signer.FormattedAddress,
		State:               trustedState,
		StateTransitionVkey: stateVkey,
		StateMembershipVkey: messageVkey,
		MerkleTreeAddress:   merkleTreeAddr,
		Groth16Vkey:         groth16Vkey,
	}

	broadcaster := cosmos.NewBroadcaster(chain)
	txResp, err := broadcaster.BroadcastMessages(ctx, signer, &txMsg)
	s.Require().NoError(err)
	s.Require().Equalf(uint32(0), txResp.Code, "tx failed: code=%d, log=%s", txResp.Code, txResp.RawLog)

	for _, evt := range txResp.Events {
		if evt.GetType() == proto.MessageName(&zkismtypes.EventCreateInterchainSecurityModule{}) {
			event, err := sdk.ParseTypedEvent(evt)
			s.Require().NoError(err)

			ismEvent, ok := event.(*zkismtypes.EventCreateInterchainSecurityModule)
			s.Require().True(ok)

			return ismEvent.Id
		}
	}

	return hyputil.NewZeroAddress()
}

func (s *HyperlaneTestSuite) QueryZKIsmMessages(ctx context.Context, chain *cosmos.Chain, ismID string) []string {
	s.T().Helper()

	networkInfo, err := chain.GetNetworkInfo(ctx)
	s.Require().NoError(err)

	grpcAddress := networkInfo.External.GRPCAddress()
	grpcConn, err := grpc.NewClient(grpcAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	s.Require().NoError(err)

	defer grpcConn.Close()

	req := &zkismtypes.QueryMessagesRequest{
		Id: ismID,
	}

	client := zkismtypes.NewQueryClient(grpcConn)
	resp, err := client.Messages(ctx, req)
	s.Require().NoError(err)

	return resp.Messages
}

func (s *HyperlaneTestSuite) ParseUpdateISMEvent(txResp sdk.TxResponse) *zkismtypes.EventUpdateInterchainSecurityModule {
	s.T().Helper()

	eventType := proto.MessageName(&zkismtypes.EventUpdateInterchainSecurityModule{})
	for _, evt := range txResp.Events {
		if evt.GetType() != eventType {
			continue
		}

		event, err := sdk.ParseTypedEvent(evt)
		s.Require().NoError(err)

		ismEvent, ok := event.(*zkismtypes.EventUpdateInterchainSecurityModule)
		s.Require().True(ok)

		return ismEvent
	}

	return nil
}

func waitForReady(t *testing.T, ctx context.Context, node *evmsingle.Node) {
	t.Helper()
	networkInfo, err := node.GetNetworkInfo(ctx)
	require.NoError(t, err)

	healthcheck := fmt.Sprintf("http://0.0.0.0:%s/health/ready", networkInfo.External.Ports.RPC)
	require.Eventually(t, func() bool {
		req, _ := http.NewRequest(http.MethodGet, healthcheck, nil)
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return false
		}
		defer func() { _ = resp.Body.Close() }()
		return resp.StatusCode == http.StatusOK
	}, time.Minute, 2*time.Second, "evm sequencer %s failed to respond healthy", node.Name())
}

func readTransferRemoteABI() (abi.ABI, error) {
	f, err := os.Open("internal/testdata/HypTokenRouterABI.json")
	if err != nil {
		return abi.ABI{}, err
	}

	defer f.Close()

	return abi.JSON(f)
}

func bech32ToBytes(address string) ([32]byte, error) {
	bz := sdk.MustAccAddressFromBech32(address).Bytes()

	if len(bz) > 32 {
		return [32]byte{}, fmt.Errorf("recipient too long: %d bytes", len(bz))
	}

	var bytes32 [32]byte
	copy(bytes32[32-len(bz):], bz)

	return bytes32, nil
}

func readGroth16Vkey(t *testing.T) []byte {
	t.Helper()

	groth16Vkey, err := os.ReadFile("internal/testdata/zkism/groth16_vk.bin")
	require.NoError(t, err, "failed to read verifier key file")

	return groth16Vkey
}

func readStateTransitionProofData(t *testing.T) ([]byte, []byte) {
	t.Helper()

	proofBz, err := os.ReadFile("internal/testdata/zkism/state_transition/proof.bin")
	require.NoError(t, err, "failed to read proof file")

	inputsBz, err := os.ReadFile("internal/testdata/zkism/state_transition/public_values.bin")
	require.NoError(t, err, "failed to read proof file")

	return proofBz, inputsBz
}

func readTrustedState(t *testing.T, pathSuffix string) []byte {
	t.Helper()

	path := fmt.Sprintf("internal/testdata/zkism/%s", pathSuffix)
	trustedStateHex, err := os.ReadFile(path)
	require.NoError(t, err, "failed to read trusted state file")

	trustedState, err := hex.DecodeString(strings.TrimSpace(string(trustedStateHex)))
	require.NoError(t, err, "failed to decode trusted state hex")

	return trustedState
}

func readStateMembershipProofData(t *testing.T) ([]byte, []byte) {
	t.Helper()

	proofBz, err := os.ReadFile("internal/testdata/zkism/state_membership/proof.bin")
	require.NoError(t, err, "failed to read proof file")

	inputsBz, err := os.ReadFile("internal/testdata/zkism/state_membership/public_values.bin")
	require.NoError(t, err, "failed to read proof file")

	return proofBz, inputsBz
}

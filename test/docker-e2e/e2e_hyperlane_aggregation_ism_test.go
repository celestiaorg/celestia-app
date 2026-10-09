package docker_e2e

import (
	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
	"context"
	"testing"

	sdkmath "cosmossdk.io/math"
	hyputil "github.com/bcp-innovations/hyperlane-cosmos/util"
	ismtypes "github.com/bcp-innovations/hyperlane-cosmos/x/core/01_interchain_security/types"
	warptypes "github.com/bcp-innovations/hyperlane-cosmos/x/warp/types"
	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	"github.com/celestiaorg/tastora/framework/docker/hyperlane"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/cosmos/gogoproto/proto"
	ethcommon "github.com/ethereum/go-ethereum/common"
)

// TestHyperlaneAggregationISMTokenTransfer runs the same flow as TestHyperlaneTokenTransfer, but the celestia
// collateral token is secured by a 2-of-3 Aggregation ISM over Noop ISMs. The relayer has to resolve the
// Aggregation ISM, build the metadata of its sub-ISMs and encode the aggregation metadata for the inbound
// transfer from reth to be delivered.
//
// Released relayers do not support Aggregation ISMs on cosmos-native chains yet, so this test requires a
// relayer image with that support (HYPERLANE_AGENT_IMAGE), built from the hyperlane-monorepo branch
// jonas/cosmos-aggregation-ism.
func (s *HyperlaneTestSuite) TestHyperlaneAggregationISMTokenTransfer() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping hyperlane aggregation ism test in short mode")
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

	// Secure the celestia collateral token with a 2-of-3 Aggregation ISM
	modules := []hyputil.HexAddress{s.CreateNoopISM(ctx, chain), s.CreateNoopISM(ctx, chain), s.CreateNoopISM(ctx, chain)}
	aggregationIsmID := s.CreateAggregationISM(ctx, chain, modules, 2)
	s.SetTokenISM(ctx, chain, config.TokenID, aggregationIsmID)

	tokenRouter, err := hyp.GetEVMWarpTokenAddress()
	s.Require().NoError(err)

	// Register Hyperlane token router connections between celestia and the evm chain
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

	// Execute the hyperlane warp transfer from reth to celestia, verified by the Aggregation ISM
	amount := sdkmath.NewInt(500)

	celestiaDomain := s.GetDomainForChain(ctx, HypCelestiaChainName, hyp)
	celestiaRecipient, err := bech32ToBytes(wallet.FormattedAddress)
	s.Require().NoError(err)

	s.SendTransferRemoteTxEvm(ctx, reth, tokenRouter, celestiaDomain, celestiaRecipient, amount)

	expBalance := balance.Add(amount)
	s.AssertBankBalance(ctx, chain, wallet.FormattedAddress, chain.Config.Denom, expBalance)
}

// CreateNoopISM creates a Noop ISM owned by the faucet wallet.
func (s *HyperlaneTestSuite) CreateNoopISM(ctx context.Context, chain *cosmos.Chain) hyputil.HexAddress {
	s.T().Helper()

	signer := chain.GetFaucetWallet()
	txResp := s.broadcastHyperlaneTx(ctx, chain, &ismtypes.MsgCreateNoopIsm{Creator: signer.FormattedAddress})

	var event ismtypes.EventCreateNoopIsm
	s.parseHyperlaneEvent(txResp, &event)
	return event.IsmId
}

// CreateAggregationISM creates an Aggregation ISM owned by the faucet wallet.
func (s *HyperlaneTestSuite) CreateAggregationISM(ctx context.Context, chain *cosmos.Chain, modules []hyputil.HexAddress, threshold uint32) hyputil.HexAddress {
	s.T().Helper()

	signer := chain.GetFaucetWallet()
	txResp := s.broadcastHyperlaneTx(ctx, chain, &ismtypes.MsgCreateAggregationIsm{
		Creator:   signer.FormattedAddress,
		Modules:   modules,
		Threshold: threshold,
	})

	var event ismtypes.EventCreateAggregationIsm
	s.parseHyperlaneEvent(txResp, &event)
	s.Require().Equal(modules, event.Modules)
	s.Require().Equal(threshold, event.Threshold)
	return event.IsmId
}

// SetTokenISM sets the ISM of a warp token owned by the faucet wallet.
func (s *HyperlaneTestSuite) SetTokenISM(ctx context.Context, chain *cosmos.Chain, tokenID, ismID hyputil.HexAddress) {
	s.T().Helper()

	signer := chain.GetFaucetWallet()
	s.broadcastHyperlaneTx(ctx, chain, &warptypes.MsgSetToken{
		Owner:   signer.FormattedAddress,
		TokenId: tokenID,
		IsmId:   &ismID,
	})
}

func (s *HyperlaneTestSuite) broadcastHyperlaneTx(ctx context.Context, chain *cosmos.Chain, msg sdk.Msg) sdk.TxResponse {
	s.T().Helper()

	broadcaster := cosmos.NewBroadcaster(chain)
	txResp, err := broadcaster.BroadcastMessages(ctx, chain.GetFaucetWallet(), msg)
	s.Require().NoError(err)
	s.Require().Equalf(uint32(0), txResp.Code, "tx failed: code=%d, log=%s", txResp.Code, txResp.RawLog)
	return txResp
}

// parseHyperlaneEvent finds the typed event of the type of out in the tx events and unpacks it into out.
func (s *HyperlaneTestSuite) parseHyperlaneEvent(txResp sdk.TxResponse, out proto.Message) {
	s.T().Helper()

	for _, evt := range txResp.Events {
		if evt.GetType() != proto.MessageName(out) {
			continue
		}
		event, err := sdk.ParseTypedEvent(evt)
		s.Require().NoError(err)
		bz, err := proto.Marshal(event)
		s.Require().NoError(err)
		s.Require().NoError(proto.Unmarshal(bz, out))
		return
	}
	s.FailNowf("event not found", "no %s event in tx %s", proto.MessageName(out), txResp.TxHash)
}

package docker_e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v8/pkg/user"
	"github.com/celestiaorg/celestia-app/v8/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v8/test/util/testnode"
	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	"github.com/celestiaorg/tastora/framework/docker/ibc"
	"github.com/celestiaorg/tastora/framework/docker/ibc/relayer"
	"github.com/celestiaorg/tastora/framework/testutil/query"
	"github.com/celestiaorg/tastora/framework/testutil/sdkacc"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
	tastoratypes "github.com/celestiaorg/tastora/framework/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	ibctransfertypes "github.com/cosmos/ibc-go/v8/modules/apps/transfer/types"
	ibcclienttypes "github.com/cosmos/ibc-go/v8/modules/core/02-client/types"
	"github.com/stretchr/testify/suite"
	"golang.org/x/sync/errgroup"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
)

const (
	baseDenom   = "utia"
	sendAmount  = int64(100_000)
	feePaid     = int64(6_500)
	txGasLimit  = 250_000
	ibcTimeout  = time.Hour
	testTimeout = 15 * time.Minute
)

type PacketMetadata struct {
	Forward *ForwardMetadata `json:"forward"`
}

type ForwardMetadata struct {
	Receiver string        `json:"receiver"`
	Port     string        `json:"port"`
	Channel  string        `json:"channel"`
	Timeout  time.Duration `json:"timeout"`
	Next     *string       `json:"next,omitempty"`
}

type PFMTestSuite struct {
	IBCTestSuite

	chainA *cosmos.Chain
	chainB *cosmos.Chain
	chainC *cosmos.Chain
	hermes *relayer.Hermes

	connAToB ibc.Connection
	chAToB   ibc.Channel
	connBToC ibc.Connection
	chBToC   ibc.Channel

	txClientA *user.TxClient
}

type hop struct {
	port      string
	channelID string
}

func TestPFMTestSuite(t *testing.T) {
	suite.Run(t, new(PFMTestSuite))
}

// TestPFMMultiHop sends tokens from chain A to chain C via chain B using PFM and
// asserts the funds arrive over the two-hop denom (A->B->C) while no direct A->C
// denom is minted.
func (s *PFMTestSuite) TestPFMMultiHop() {
	if testing.Short() {
		s.T().Skip("skipping PFM test in short mode")
	}

	ctx, cancel := context.WithTimeout(context.Background(), testTimeout)
	defer cancel()

	s.setupPFMInfrastructure(ctx)

	walletA := s.chainA.GetFaucetWallet()
	walletB := s.chainB.GetFaucetWallet()
	walletC := s.chainC.GetFaucetWallet()
	addrC, err := sdkacc.AddressFromWallet(walletC)
	s.Require().NoError(err)

	// Create PFM memo: chain-a sends to chain-b, which forwards to chain-c
	memoJSON := makePFMMemo(addrC.String(), s.chBToC.PortID, s.chBToC.ChannelID, nil)

	initialChainABalance := s.getBalance(ctx, s.chainA, walletA.GetFormattedAddress(), baseDenom)
	denomOnB := calculateIBCDenomTrace(
		baseDenom,
		hop{port: s.chAToB.CounterpartyPort, channelID: s.chAToB.CounterpartyID},
	)
	initialChainBRelayBalance := s.getBalance(ctx, s.chainB, walletB.GetFormattedAddress(), denomOnB)

	denomOnC := calculateIBCDenomTrace(
		baseDenom,
		hop{port: s.chAToB.CounterpartyPort, channelID: s.chAToB.CounterpartyID},
		hop{port: s.chBToC.CounterpartyPort, channelID: s.chBToC.CounterpartyID},
	)
	initialChainCRecipientBalance := s.getBalance(ctx, s.chainC, walletC.GetFormattedAddress(), denomOnC)

	amt := sdkmath.NewInt(sendAmount)
	msg := ibctransfertypes.NewMsgTransfer(
		s.chAToB.PortID,
		s.chAToB.ChannelID,
		sdk.NewCoin(baseDenom, amt),
		walletA.GetFormattedAddress(),
		walletB.GetFormattedAddress(),
		ibcclienttypes.ZeroHeight(),
		uint64(time.Now().UTC().Add(ibcTimeout).UnixNano()),
		memoJSON,
	)

	s.T().Logf("Submitting transfer: %d %s from A to B with PFM memo to forward to C", amt.Int64(), baseDenom)
	resp, err := s.txClientA.SubmitTx(ctx, []sdk.Msg{msg}, user.SetGasLimit(txGasLimit), user.SetFee(uint64(feePaid)))
	s.Require().NoError(err)
	s.Require().Equal(uint32(0), resp.Code, "transfer failed, code=%d", resp.Code)
	s.T().Logf("Transfer submitted successfully, tx hash: %s, height: %d", resp.TxHash, resp.Height)

	expectedChainABalance := initialChainABalance.Sub(amt).SubRaw(feePaid)
	s.T().Logf("Waiting for PFM multi-hop transfer to complete...")

	err = wait.ForCondition(ctx, 3*time.Minute, 10*time.Second, func() (bool, error) {
		finalChainCRecipientBalance := s.getBalance(ctx, s.chainC, walletC.GetFormattedAddress(), denomOnC)
		delta := finalChainCRecipientBalance.Sub(initialChainCRecipientBalance)
		if !delta.Equal(amt) {
			s.T().Logf("Chain C has not received tokens yet (got %s, expected %s)", delta.String(), amt.String())
			return false, nil
		}
		s.T().Logf("Chain C received correct amount")
		return true, nil
	})

	s.Require().NoError(err, "PFM multi-hop transfer failed")

	finalChainBRelayBalance := s.getBalance(ctx, s.chainB, walletB.GetFormattedAddress(), denomOnB)
	s.Require().True(finalChainBRelayBalance.Equal(initialChainBRelayBalance), "chain-b retained funds: before=%s after=%s", initialChainBRelayBalance.String(), finalChainBRelayBalance.String())

	finalChainABalance := s.getBalance(ctx, s.chainA, walletA.GetFormattedAddress(), baseDenom)
	s.Require().True(expectedChainABalance.Equal(finalChainABalance), "chain-a balance mismatch: expected %s got %s", expectedChainABalance.String(), finalChainABalance.String())

	s.T().Logf("Verifying PFM used two-hop path (not direct A->C)")

	twoHopDenom := calculateIBCDenomTrace(
		baseDenom,
		hop{port: s.chAToB.CounterpartyPort, channelID: s.chAToB.CounterpartyID},
		hop{port: s.chBToC.CounterpartyPort, channelID: s.chBToC.CounterpartyID},
	)
	finalTwoHopRecipientBalance := s.getBalance(ctx, s.chainC, walletC.GetFormattedAddress(), twoHopDenom)
	s.T().Logf("Two-hop path (A->B->C) balance: %s %s", finalTwoHopRecipientBalance.String(), twoHopDenom)

	s.assertOnlyTwoHopDenom(ctx, s.chainC, walletC.GetFormattedAddress(), twoHopDenom)

	if finalTwoHopRecipientBalance.Equal(sdkmath.NewInt(sendAmount)) {
		s.T().Logf("PFM used two-hop path (A->B->C)")
	} else {
		s.T().Fatalf("expected %d via two-hop path, got %s", sendAmount, finalTwoHopRecipientBalance.String())
	}
}

func (s *PFMTestSuite) TearDownTest() {
	ctx := context.Background()

	chains := []*cosmos.Chain{s.chainA, s.chainB, s.chainC}
	for _, chain := range chains {
		if chain == nil {
			continue
		}
		if err := chain.Remove(ctx); err != nil {
			s.T().Logf("Error stopping chain %s: %v", chain.GetChainID(), err)
		}
	}

	if s.hermes != nil {
		if err := s.hermes.Remove(ctx); err != nil {
			s.T().Logf("Error stopping hermes: %v", err)
		}
	}
}

func (s *PFMTestSuite) getAllBalances(ctx context.Context, chain *cosmos.Chain, address string) sdk.Coins {
	node := chain.GetNode()
	if node.GrpcConn == nil {
		s.T().Logf("GRPC connection is nil for chain %s, returning empty balances", chain.GetChainID())
		return sdk.Coins{}
	}

	resp, err := banktypes.NewQueryClient(node.GrpcConn).AllBalances(ctx, &banktypes.QueryAllBalancesRequest{Address: address})
	s.Require().NoError(err, "failed to query balances for %s on %s", address, chain.GetChainID())

	balances := make(sdk.Coins, 0, len(resp.Balances))
	for _, coin := range resp.Balances {
		amount, err := query.Balance(ctx, node.GrpcConn, address, coin.Denom)
		s.Require().NoError(err, "failed to query balance for %s denom %s", address, coin.Denom)
		balances = append(balances, sdk.Coin{
			Denom:  coin.Denom,
			Amount: amount,
		})
	}

	return balances
}

// assertOnlyTwoHopDenom ensures no unexpected IBC denom holds funds.
func (s *PFMTestSuite) assertOnlyTwoHopDenom(ctx context.Context, chain *cosmos.Chain, address, expectedDenom string) {
	balances := s.getAllBalances(ctx, chain, address)

	for _, coin := range balances {
		if strings.HasPrefix(coin.Denom, "ibc/") && coin.Denom != expectedDenom && coin.Amount.GT(sdkmath.ZeroInt()) {
			s.T().Fatalf("unexpected IBC denom on %s: %s amount=%s", chain.GetChainID(), coin.Denom, coin.Amount.String())
		}
	}
}

// setupPFMInfrastructure sets up three chains and IBC routing required for PFM.
func (s *PFMTestSuite) setupPFMInfrastructure(ctx context.Context) {
	t := s.T()

	tag, err := dockerchain.GetCelestiaTagStrict()
	s.Require().NoError(err, "failed to get celestia tag")

	g, egCtx := errgroup.WithContext(ctx)
	g.Go(func() error {
		chain, err := s.buildChain(egCtx, "chain-a", tag)
		if err != nil {
			return fmt.Errorf("chain-a: %w", err)
		}
		s.chainA = chain
		return nil
	})
	g.Go(func() error {
		chain, err := s.buildChain(egCtx, "chain-b", tag)
		if err != nil {
			return fmt.Errorf("chain-b: %w", err)
		}
		s.chainB = chain
		return nil
	})
	g.Go(func() error {
		chain, err := s.buildChain(egCtx, "chain-c", tag)
		if err != nil {
			return fmt.Errorf("chain-c: %w", err)
		}
		s.chainC = chain
		return nil
	})
	s.Require().NoError(g.Wait(), "failed to build celestia chains")

	s.hermes, err = relayer.NewHermes(ctx, s.client, t.Name(), s.network, 0, s.logger)
	s.Require().NoError(err, "failed to create hermes")
	err = s.hermes.Init(ctx, []tastoratypes.Chain{s.chainA, s.chainB, s.chainC})
	s.Require().NoError(err, "failed to initialize hermes")

	s.Require().NoError(s.hermes.CreateClients(ctx, s.chainA, s.chainB))
	s.Require().NoError(s.hermes.CreateClients(ctx, s.chainB, s.chainC))

	s.connAToB, s.chAToB = s.establishConnectionAndChannel(ctx, s.chainA, s.chainB)
	s.connBToC, s.chBToC = s.establishConnectionAndChannel(ctx, s.chainB, s.chainC)

	s.Require().NoError(s.hermes.Start(ctx), "failed to start hermes relayer")

	s.txClientA, err = s.setupTxClient(ctx, s.chainA)
	s.Require().NoError(err, "failed to setup tx client for chain-a")
}

// buildChain creates and starts a single celestia-app chain with one validator.
func (s *PFMTestSuite) buildChain(ctx context.Context, chainID, tag string) (*cosmos.Chain, error) {
	cfg := &dockerchain.Config{
		Config: &testnode.Config{
			Genesis: genesis.NewDefaultGenesis().
				WithChainID(chainID).
				WithValidators(genesis.NewDefaultValidator("validator-0")).
				WithConsensusParams(testnode.DefaultConsensusParams()),
			UniversalTestingConfig: testnode.UniversalTestingConfig{
				TmConfig:     testnode.DefaultTendermintConfig(),
				AppCreator:   testnode.DefaultAppCreator(),
				AppConfig:    testnode.DefaultAppConfig(),
				AppOptions:   testnode.DefaultAppOptions(),
				SuppressLogs: true,
			},
		},
		Image:           dockerchain.GetCelestiaImage(),
		Tag:             tag,
		DockerClient:    s.client,
		DockerNetworkID: s.network,
	}

	chain, err := dockerchain.NewCelestiaChainBuilder(s.T(), cfg).Build(ctx)
	if err != nil {
		return nil, fmt.Errorf("build chain %s: %w", chainID, err)
	}
	if err := chain.Start(ctx); err != nil {
		return nil, fmt.Errorf("start chain %s: %w", chainID, err)
	}

	return chain, nil
}

// establishConnectionAndChannel creates a connection and ICS20 channel between two chains.
func (s *PFMTestSuite) establishConnectionAndChannel(ctx context.Context, a, b *cosmos.Chain) (ibc.Connection, ibc.Channel) {
	connection, err := s.hermes.CreateConnections(ctx, a, b)
	s.Require().NoError(err, "failed to create IBC connection")

	channelOpts := ibc.CreateChannelOptions{
		SourcePortName: ibctransfertypes.PortID,
		DestPortName:   ibctransfertypes.PortID,
		Order:          ibc.OrderUnordered,
		Version:        ibctransfertypes.Version,
	}

	channel, err := s.hermes.CreateChannel(ctx, a, connection, channelOpts)
	s.Require().NoError(err, "failed to create IBC channel")
	return connection, channel
}

// makePFMMemo creates a PFM forward memo JSON string.
func makePFMMemo(receiver, port, channel string, next *string) string {
	b, err := json.Marshal(&PacketMetadata{Forward: &ForwardMetadata{
		Receiver: receiver,
		Port:     port,
		Channel:  channel,
		Timeout:  0,
		Next:     next,
	}})
	if err != nil {
		panic(fmt.Sprintf("failed to marshal PFM memo: %v", err))
	}
	return string(b)
}

// calculateIBCDenomTrace calculates the IBC denom for multi-hop transfers.
func calculateIBCDenomTrace(baseDenom string, hops ...hop) string {
	denom := baseDenom
	for _, hop := range hops {
		denom = ibctransfertypes.GetPrefixedDenom(hop.port, hop.channelID, denom)
	}
	return ibctransfertypes.ParseDenomTrace(denom).IBCDenom()
}

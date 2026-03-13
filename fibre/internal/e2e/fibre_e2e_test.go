package e2e_test

import (
	"context"
	"crypto/rand"
	"fmt"
	"log/slog"
	"path/filepath"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v8/app"
	"github.com/celestiaorg/celestia-app/v8/app/encoding"
	"github.com/celestiaorg/celestia-app/v8/fibre"
	grpcfibre "github.com/celestiaorg/celestia-app/v8/fibre/internal/grpc"
	"github.com/celestiaorg/celestia-app/v8/fibre/validator"
	"github.com/celestiaorg/celestia-app/v8/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v8/pkg/user"
	"github.com/celestiaorg/celestia-app/v8/test/util/testnode"
	fibretypes "github.com/celestiaorg/celestia-app/v8/x/fibre/types"
	valtypes "github.com/celestiaorg/celestia-app/v8/x/valaddr/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cometbft/cometbft/privval"
	core "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestFibreE2ETestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping fibre e2e test in short mode")
	}
	suite.Run(t, new(FibreE2ETestSuite))
}

type FibreE2ETestSuite struct {
	suite.Suite

	cctx testnode.Context

	fibreServer  *fibre.Server
	hostRegistry *grpcfibre.HostRegistry
	fibreClient  *fibre.Client
	txClient     *user.TxClient
}

func (s *FibreE2ETestSuite) SetupSuite() {
	t := s.T()

	cfg := testnode.DefaultConfig().
		WithFundedAccounts(fibre.DefaultKeyName).
		WithDelayedPrecommitTimeout(500 * time.Millisecond)

	cctx, _, grpcAddr := testnode.NewNetwork(t, cfg)
	s.cctx = cctx

	_, err := s.cctx.WaitForHeight(1)
	require.NoError(t, err, "failed to wait for first block")

	// start fibre server
	pvKeyFile := filepath.Join(s.cctx.HomeDir, "config", "priv_validator_key.json")
	pvStateFile := filepath.Join(s.cctx.HomeDir, "data", "priv_validator_state.json")
	filePV := privval.LoadFilePV(pvKeyFile, pvStateFile)

	serverCfg := fibre.DefaultServerConfig()
	serverCfg.AppGRPCAddress = grpcAddr
	serverCfg.ServerListenAddress = "127.0.0.1:0"
	serverCfg.SignerFn = func(_ string) (core.PrivValidator, error) {
		return filePV, nil
	}

	serverCfg.StoreFn = func(scfg fibre.StoreConfig) (*fibre.Store, error) {
		return fibre.NewMemoryStore(scfg), nil
	}
	s.fibreServer, err = fibre.NewServer(serverCfg)
	require.NoError(t, err)
	require.NoError(t, s.fibreServer.Start(s.cctx.GoContext()))

	// create fibre client
	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	txClient, err := user.SetupTxClient(
		s.cctx.GoContext(),
		s.cctx.Keyring,
		s.cctx.GRPCClient,
		ecfg,
		user.WithDefaultAccount(fibre.DefaultKeyName),
	)
	require.NoError(t, err)

	s.hostRegistry = grpcfibre.NewHostRegistry(valtypes.NewQueryClient(s.cctx.GRPCClient), slog.Default())

	clientCfg := fibre.DefaultClientConfig()
	clientCfg.StateAddress = grpcAddr
	// connect all validators to the single fibre server address
	fibreAddr := s.fibreServer.ListenAddress()
	clientCfg.NewClientFn = grpcfibre.DefaultNewClientFn(
		&fixedHostRegistry{addr: fibreAddr},
		clientCfg.MaxMessageSize,
	)

	s.txClient = txClient
	s.fibreClient, err = fibre.NewClient(s.cctx.Keyring, clientCfg)
	require.NoError(t, err)

	require.NoError(t, s.fibreClient.Start(s.cctx.GoContext()))
}

func (s *FibreE2ETestSuite) TearDownSuite() {
	if s.fibreServer != nil {
		_ = s.fibreServer.Stop(context.Background())
	}
	if s.fibreClient != nil {
		_ = s.fibreClient.Stop(context.Background())
	}
}

func (s *FibreE2ETestSuite) Test01RegisterValidator() {
	t := s.T()
	ctx := s.cctx.GoContext()

	// get the validator's operator address from the staking module.
	stakingClient := stakingtypes.NewQueryClient(s.cctx.GRPCClient)
	validatorsResp, err := stakingClient.Validators(ctx, &stakingtypes.QueryValidatorsRequest{})
	require.NoError(t, err)
	require.Len(t, validatorsResp.Validators, 1)

	valOperatorAddr := validatorsResp.Validators[0].OperatorAddress

	// submit MsgSetFibreProviderInfo to register the fibre server's gRPC address.
	txClient, err := testnode.NewTxClientFromContext(s.cctx)
	require.NoError(t, err)

	fibreAddr := s.fibreServer.ListenAddress()
	msg := &valtypes.MsgSetFibreProviderInfo{
		Signer: valOperatorAddr,
		Host:   fibreAddr,
	}

	txResp, err := txClient.SubmitTx(ctx, []sdk.Msg{msg}, user.SetGasLimit(200_000), user.SetFee(5_000))
	require.NoError(t, err)
	require.Equal(t, uint32(0), txResp.Code)
	t.Logf("RegisterValidator tx included at height %d, hash: %s", txResp.Height, txResp.TxHash)

	require.NoError(t, s.cctx.WaitForNextBlock())

	// verify the host is now registered.
	valAddrClient := valtypes.NewQueryClient(s.cctx.GRPCClient)

	// derive the validator consensus address via the cometbft service.
	tmServiceClient := cmtservice.NewServiceClient(s.cctx.GRPCClient)
	valSetResp, err := tmServiceClient.GetLatestValidatorSet(ctx, &cmtservice.GetLatestValidatorSetRequest{})
	require.NoError(t, err)
	require.Len(t, valSetResp.Validators, 1)
	consAddr, err := sdk.ConsAddressFromBech32(valSetResp.Validators[0].Address)
	require.NoError(t, err)

	resp, err := valAddrClient.FibreProviderInfo(ctx, &valtypes.QueryFibreProviderInfoRequest{
		ValidatorConsensusAddress: consAddr.String(),
	})
	require.NoError(t, err)
	require.True(t, resp.Found)
	require.Equal(t, fibreAddr, resp.Info.Host)

	// refresh the host registry so the client can find the validator.
	err = s.hostRegistry.Start(ctx)
	require.NoError(t, err)
}

func (s *FibreE2ETestSuite) Test02FundEscrowAccount() {
	t := s.T()
	ctx := s.cctx.GoContext()

	// get client address from the keyring.
	keyInfo, err := s.cctx.Keyring.Key(fibre.DefaultKeyName)
	require.NoError(t, err)
	addr, err := keyInfo.GetAddress()
	require.NoError(t, err)

	fibreQueryClient := fibretypes.NewQueryClient(s.cctx.GRPCClient)

	// verify escrow account doesn't exist yet.
	escrowResp, err := fibreQueryClient.EscrowAccount(ctx, &fibretypes.QueryEscrowAccountRequest{
		Signer: addr.String(),
	})
	require.NoError(t, err)
	require.False(t, escrowResp.Found)

	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	txClient, err := user.SetupTxClient(
		ctx, s.cctx.Keyring, s.cctx.GRPCClient, ecfg,
		user.WithDefaultAccount(fibre.DefaultKeyName),
	)
	require.NoError(t, err)

	// first deposit: 50 TIA (50_000_000 utia).
	depositAmount := sdk.NewCoin(appconsts.BondDenom, sdkmath.NewInt(50_000_000))
	msg := &fibretypes.MsgDepositToEscrow{
		Signer: addr.String(),
		Amount: depositAmount,
	}
	txResp, err := txClient.SubmitTx(ctx, []sdk.Msg{msg}, user.SetGasLimit(200_000), user.SetFee(5_000))
	require.NoError(t, err)
	require.Equal(t, uint32(0), txResp.Code)
	t.Logf("First deposit tx at height %d", txResp.Height)

	// verify escrow balance matches deposit.
	escrowResp, err = fibreQueryClient.EscrowAccount(ctx, &fibretypes.QueryEscrowAccountRequest{
		Signer: addr.String(),
	})
	require.NoError(t, err)
	require.True(t, escrowResp.Found)
	require.Equal(t, depositAmount, escrowResp.EscrowAccount.Balance)

	// second deposit: 25 TIA (25_000_000 utia).
	depositAmount2 := sdk.NewCoin(appconsts.BondDenom, sdkmath.NewInt(25_000_000))
	msg2 := &fibretypes.MsgDepositToEscrow{
		Signer: addr.String(),
		Amount: depositAmount2,
	}
	txResp, err = txClient.SubmitTx(ctx, []sdk.Msg{msg2}, user.SetGasLimit(200_000), user.SetFee(5_000))
	require.NoError(t, err)
	require.Equal(t, uint32(0), txResp.Code)
	t.Logf("Second deposit tx at height %d", txResp.Height)

	// verify cumulative balance is 75 TIA.
	escrowResp, err = fibreQueryClient.EscrowAccount(ctx, &fibretypes.QueryEscrowAccountRequest{
		Signer: addr.String(),
	})
	require.NoError(t, err)
	require.True(t, escrowResp.Found)
	expectedBalance := sdk.NewCoin(appconsts.BondDenom, sdkmath.NewInt(75_000_000))
	require.Equal(t, expectedBalance, escrowResp.EscrowAccount.Balance)
}

func (s *FibreE2ETestSuite) Test03Put() {
	t := s.T()
	ctx := s.cctx.GoContext()

	// wait for a fresh block to avoid clock skew with payment promise.
	err := s.cctx.WaitForNextBlock()
	require.NoError(t, err)

	// generate 4 KiB of random test data.
	testData := make([]byte, 4*1024)
	_, err = rand.Read(testData)
	require.NoError(t, err)

	ns := share.MustNewV0Namespace([]byte{0xDE, 0xAD})

	result, err := fibre.Put(ctx, s.fibreClient, s.txClient, ns, testData)
	require.NoError(t, err)

	// verify Put result.
	require.NotEmpty(t, result.BlobID.Commitment().String(), "commitment should not be empty")
	require.NotEmpty(t, result.ValidatorSignatures, "should have validator signatures")
	require.NotEmpty(t, result.TxHash, "tx hash should not be empty")
	require.Greater(t, result.Height, uint64(0), "height should be positive")
	t.Logf("Put result: commitment=%s, txHash=%s, height=%d", result.BlobID.String(), result.TxHash, result.Height)

	// verify data was stored in server's store.
	shard, err := s.fibreServer.Store().Get(ctx, result.BlobID.Commitment())
	require.NoError(t, err)
	require.NotNil(t, shard)
	require.NotEmpty(t, shard.Rows, "stored shard should have rows")
	require.NotNil(t, shard.GetRoot(), "stored shard should have an RLC root")
	require.Len(t, shard.GetRoot(), 32, "RLC root should be 32 bytes")

	// verify the PayForFibre tx was included on chain by waiting for the block.
	_, err = s.cctx.WaitForTx(result.TxHash, 5)
	require.NoError(t, err)
}

// fixedHostRegistry returns the same address for every validator.
type fixedHostRegistry struct{ addr string }

func (r *fixedHostRegistry) GetHost(_ context.Context, _ *core.Validator) (validator.Host, error) {
	if r.addr == "" {
		return "", fmt.Errorf("no address configured")
	}
	return validator.Host(r.addr), nil
}

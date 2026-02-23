package fibre_test

import (
	"context"
	"crypto/rand"
	"net"
	"path/filepath"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app-fibre/v6/app"
	"github.com/celestiaorg/celestia-app-fibre/v6/app/encoding"
	"github.com/celestiaorg/celestia-app-fibre/v6/fibre"
	grpcfibre "github.com/celestiaorg/celestia-app-fibre/v6/fibre/grpc"
	"github.com/celestiaorg/celestia-app-fibre/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app-fibre/v6/pkg/user"
	"github.com/celestiaorg/celestia-app-fibre/v6/test/util/testnode"
	fibretypes "github.com/celestiaorg/celestia-app-fibre/v6/x/fibre/types"
	valtypes "github.com/celestiaorg/celestia-app-fibre/v6/x/valaddr/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cometbft/cometbft/privval"
	coregrpc "github.com/cometbft/cometbft/rpc/grpc"
	core "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestFibreE2ETestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping fibre e2e test in short mode")
	}
	suite.Run(t, new(FibreE2ETestSuite))
}

type FibreE2ETestSuite struct {
	suite.Suite

	ecfg encoding.Config
	cctx testnode.Context

	fibreServer *fibre.Server
	grpcServer  *grpc.Server
	grpcAddr    string

	valSetGetter *grpcfibre.SetGetter
	hostRegistry *grpcfibre.HostRegistry

	fibreClient *fibre.Client
}

func (s *FibreE2ETestSuite) SetupSuite() {
	t := s.T()

	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)

	cfg := testnode.DefaultConfig().
		WithFundedAccounts(fibre.DefaultKeyName).
		WithDelayedPrecommitTimeout(500 * time.Millisecond)

	cctx, _, _ := testnode.NewNetwork(t, cfg)
	s.cctx = cctx

	_, err := s.cctx.WaitForHeight(1)
	require.NoError(t, err, "failed to wait for first block")

	s.setupFibreServer(t)
	s.setupFibreClient(t)
}

func (s *FibreE2ETestSuite) setupFibreServer(t *testing.T) {
	t.Helper()

	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	s.grpcAddr = listener.Addr().String()

	// Load the validator's private key from testnode config files.
	pvKeyFile := filepath.Join(s.cctx.HomeDir, "config", "priv_validator_key.json")
	pvStateFile := filepath.Join(s.cctx.HomeDir, "data", "priv_validator_state.json")
	filePV := privval.LoadFilePV(pvKeyFile, pvStateFile)

	fibreQueryClient := fibretypes.NewQueryClient(s.cctx.GRPCClient)
	s.valSetGetter = grpcfibre.NewSetGetter(coregrpc.NewBlockAPIClient(s.cctx.GRPCClient))

	serverCfg := fibre.DefaultServerConfig()
	serverCfg.ChainID = s.cctx.ChainID

	s.fibreServer, err = fibre.NewInMemoryServer(filePV, fibreQueryClient, s.valSetGetter, serverCfg)
	require.NoError(t, err)
	s.fibreServer.Start()

	maxMsgSize := serverCfg.MaxMessageSize
	s.grpcServer = grpc.NewServer(
		grpc.MaxRecvMsgSize(maxMsgSize),
		grpc.MaxSendMsgSize(maxMsgSize),
	)
	fibretypes.RegisterFibreServer(s.grpcServer, s.fibreServer)

	go func() { _ = s.grpcServer.Serve(listener) }()
}

func (s *FibreE2ETestSuite) setupFibreClient(t *testing.T) {
	t.Helper()

	txClient, err := user.SetupTxClient(
		s.cctx.GoContext(),
		s.cctx.Keyring,
		s.cctx.GRPCClient,
		s.ecfg,
		user.WithDefaultAccount(fibre.DefaultKeyName),
	)
	require.NoError(t, err)

	s.hostRegistry = grpcfibre.NewHostRegistry(valtypes.NewQueryClient(s.cctx.GRPCClient))

	clientCfg := fibre.DefaultClientConfig()
	clientCfg.ChainID = s.cctx.ChainID
	clientCfg.NewClientFn = newTestClientFn(s.grpcAddr, clientCfg.MaxMessageSize)

	s.fibreClient, err = fibre.NewClient(txClient, s.cctx.Keyring, s.valSetGetter, s.hostRegistry, clientCfg)
	require.NoError(t, err)
}

func (s *FibreE2ETestSuite) TearDownSuite() {
	if s.grpcServer != nil {
		s.grpcServer.Stop()
	}
	if s.fibreServer != nil {
		_ = s.fibreServer.Stop()
	}
	if s.fibreClient != nil {
		_ = s.fibreClient.Close()
	}
}

func (s *FibreE2ETestSuite) Test01RegisterValidator() {
	t := s.T()
	ctx := s.cctx.GoContext()

	// Get the validator's operator address from the staking module.
	stakingClient := stakingtypes.NewQueryClient(s.cctx.GRPCClient)
	validatorsResp, err := stakingClient.Validators(ctx, &stakingtypes.QueryValidatorsRequest{})
	require.NoError(t, err)
	require.Len(t, validatorsResp.Validators, 1)

	valOperatorAddr := validatorsResp.Validators[0].OperatorAddress

	// Submit MsgSetFibreProviderInfo to register the fibre server's gRPC address.
	txClient, err := testnode.NewTxClientFromContext(s.cctx)
	require.NoError(t, err)

	msg := &valtypes.MsgSetFibreProviderInfo{
		Signer: valOperatorAddr,
		Host:   s.grpcAddr,
	}

	txResp, err := txClient.SubmitTx(ctx, []sdk.Msg{msg}, user.SetGasLimit(200_000), user.SetFee(5_000))
	require.NoError(t, err)
	require.Equal(t, uint32(0), txResp.Code)
	t.Logf("RegisterValidator tx included at height %d, hash: %s", txResp.Height, txResp.TxHash)

	// Verify the host is now registered.
	valAddrClient := valtypes.NewQueryClient(s.cctx.GRPCClient)

	// Derive the validator consensus address via the cometbft service.
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
	require.Equal(t, s.grpcAddr, resp.Info.Host)

	// Refresh the host registry so the client can find the validator.
	err = s.hostRegistry.Start(ctx)
	require.NoError(t, err)
}

func (s *FibreE2ETestSuite) Test02FundEscrowAccount() {
	t := s.T()
	ctx := s.cctx.GoContext()

	// Get client address from the keyring.
	keyInfo, err := s.cctx.Keyring.Key(fibre.DefaultKeyName)
	require.NoError(t, err)
	addr, err := keyInfo.GetAddress()
	require.NoError(t, err)

	fibreQueryClient := fibretypes.NewQueryClient(s.cctx.GRPCClient)

	// Verify escrow account doesn't exist yet.
	escrowResp, err := fibreQueryClient.EscrowAccount(ctx, &fibretypes.QueryEscrowAccountRequest{
		Signer: addr.String(),
	})
	require.NoError(t, err)
	require.False(t, escrowResp.Found)

	txClient, err := user.SetupTxClient(
		ctx, s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg,
		user.WithDefaultAccount(fibre.DefaultKeyName),
	)
	require.NoError(t, err)

	// First deposit: 50 TIA (50_000_000 utia).
	depositAmount := sdk.NewCoin(appconsts.BondDenom, sdkmath.NewInt(50_000_000))
	msg := &fibretypes.MsgDepositToEscrow{
		Signer: addr.String(),
		Amount: depositAmount,
	}
	txResp, err := txClient.SubmitTx(ctx, []sdk.Msg{msg}, user.SetGasLimit(200_000), user.SetFee(5_000))
	require.NoError(t, err)
	require.Equal(t, uint32(0), txResp.Code)
	t.Logf("First deposit tx at height %d", txResp.Height)

	// Verify escrow balance matches deposit.
	escrowResp, err = fibreQueryClient.EscrowAccount(ctx, &fibretypes.QueryEscrowAccountRequest{
		Signer: addr.String(),
	})
	require.NoError(t, err)
	require.True(t, escrowResp.Found)
	require.Equal(t, depositAmount, escrowResp.EscrowAccount.Balance)

	// Second deposit: 25 TIA (25_000_000 utia).
	depositAmount2 := sdk.NewCoin(appconsts.BondDenom, sdkmath.NewInt(25_000_000))
	msg2 := &fibretypes.MsgDepositToEscrow{
		Signer: addr.String(),
		Amount: depositAmount2,
	}
	txResp, err = txClient.SubmitTx(ctx, []sdk.Msg{msg2}, user.SetGasLimit(200_000), user.SetFee(5_000))
	require.NoError(t, err)
	require.Equal(t, uint32(0), txResp.Code)
	t.Logf("Second deposit tx at height %d", txResp.Height)

	// Verify cumulative balance is 75 TIA.
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

	// Wait for a fresh block to avoid clock skew with payment promise.
	err := s.cctx.WaitForNextBlock()
	require.NoError(t, err)

	// Generate 4 KiB of random test data.
	testData := make([]byte, 4*1024)
	_, err = rand.Read(testData)
	require.NoError(t, err)

	ns := share.MustNewV0Namespace([]byte{0xDE, 0xAD})

	result, err := s.fibreClient.Put(ctx, ns, testData)
	require.NoError(t, err)

	// Verify Put result.
	require.NotEmpty(t, result.BlobID.Commitment().String(), "commitment should not be empty")
	require.NotEmpty(t, result.ValidatorSignatures, "should have validator signatures")
	require.NotEmpty(t, result.TxHash, "tx hash should not be empty")
	require.Greater(t, result.Height, uint64(0), "height should be positive")
	t.Logf("Put result: commitment=%s, txHash=%s, height=%d", result.BlobID.String(), result.TxHash, result.Height)

	// Verify data was stored in server's store.
	shard, err := s.fibreServer.Store().Get(ctx, result.BlobID.Commitment())
	require.NoError(t, err)
	require.NotNil(t, shard)
	require.NotEmpty(t, shard.Rows, "stored shard should have rows")
	require.NotNil(t, shard.GetRoot(), "stored shard should have an RLC root")
	require.Len(t, shard.GetRoot(), 32, "RLC root should be 32 bytes")

	// Verify the PayForFibre tx was included on chain by waiting for the block.
	_, err = s.cctx.WaitForTx(result.TxHash, 5)
	require.NoError(t, err)
}

// newTestClientFn returns a [grpcfibre.NewClientFn] that always connects to a fixed
// address, bypassing the host registry lookup. This is needed because the
// fibre server in this test is running on a separate gRPC listener from the
// testnode's gRPC server.
func newTestClientFn(addr string, maxMsgSize int) grpcfibre.NewClientFn {
	return func(_ context.Context, _ *core.Validator) (grpcfibre.Client, error) {
		conn, err := grpc.NewClient(addr,
			grpc.WithTransportCredentials(insecure.NewCredentials()),
			grpc.WithDefaultCallOptions(
				grpc.MaxCallRecvMsgSize(maxMsgSize),
				grpc.MaxCallSendMsgSize(maxMsgSize),
			),
		)
		if err != nil {
			return nil, err
		}
		return &fibreClientCloser{
			FibreClient: fibretypes.NewFibreClient(conn),
			conn:        conn,
		}, nil
	}
}

// fibreClientCloser wraps a [fibretypes.FibreClient] and [grpc.ClientConn] to implement [grpcfibre.Client].
type fibreClientCloser struct {
	fibretypes.FibreClient
	conn *grpc.ClientConn
}

func (f *fibreClientCloser) Close() error {
	return f.conn.Close()
}

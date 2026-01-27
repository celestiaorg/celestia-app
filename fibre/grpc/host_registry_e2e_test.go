package grpc_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app-fibre/v6/app"
	"github.com/celestiaorg/celestia-app-fibre/v6/app/encoding"
	"github.com/celestiaorg/celestia-app-fibre/v6/fibre/grpc"
	"github.com/celestiaorg/celestia-app-fibre/v6/pkg/user"
	"github.com/celestiaorg/celestia-app-fibre/v6/test/util/testnode"
	"github.com/celestiaorg/celestia-app-fibre/v6/x/valaddr/types"
	core "github.com/cometbft/cometbft/types"
	"github.com/cosmos/cosmos-sdk/client/grpc/cmtservice"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestIntegrationTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping grpc host registry test in short mode.")
	}
	suite.Run(t, &IntegrationTestSuite{})
}

type IntegrationTestSuite struct {
	suite.Suite

	ecfg         encoding.Config
	cctx         testnode.Context
	hostRegistry *grpc.HostRegistry
	validator    *core.Validator
}

func (s *IntegrationTestSuite) SetupSuite() {
	t := s.T()

	cfg := testnode.DefaultConfig().WithFundedAccounts().WithDelayedPrecommitTimeout(time.Millisecond * 500)
	cctx, _, _ := testnode.NewNetwork(t, cfg)

	s.cctx = cctx
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)

	s.hostRegistry = grpc.NewHostRegistry(types.NewQueryClient(s.cctx.GRPCClient))
	err := s.hostRegistry.Start(t.Context())
	require.NoError(t, err)

	// Wait for at least one block to be produced before querying
	_, err = s.cctx.WaitForHeight(1)
	require.NoError(t, err, "failed to wait for first block")

	// Use the GRPCClient to setup a cmtservice query client and query the validator set
	tmserviceClient := cmtservice.NewServiceClient(s.cctx.GRPCClient)
	valSetResp, err := tmserviceClient.GetLatestValidatorSet(s.cctx.GoContext(), &cmtservice.GetLatestValidatorSetRequest{})
	require.NoError(t, err)
	require.NotNil(t, valSetResp)
	require.Len(t, valSetResp.Validators, 1, "validator set should have one validator")
	cmtVal := valSetResp.Validators[0]

	// Convert cmtservice.Validator address (Bech32 string) to core.Validator
	// The address is in Bech32 format (e.g., celestiavalcons...)
	consAddr, err := sdk.ConsAddressFromBech32(cmtVal.Address)
	require.NoError(t, err, "failed to decode validator consensus address")
	s.validator = &core.Validator{
		Address:     consAddr.Bytes(), // this matches PrivateKey().PubKey().Address().Bytes()
		VotingPower: cmtVal.VotingPower,
	}
}

func (s *IntegrationTestSuite) TestGetHostEmpty() {
	t := s.T()

	// Get the host for this validator
	// In a fresh testnode, validators won't have fibre provider info registered,
	// so we expect an error here
	host, err := s.hostRegistry.GetHost(s.cctx.GoContext(), s.validator)
	require.Error(t, err)
	require.Empty(t, host.String())

	// Now try with a fake validator (not in state), should error
	fakeAddr := make([]byte, 20) // Standard address length
	for i := range fakeAddr {
		fakeAddr[i] = 0xFF // Set to all FFs
	}
	fakeVal := &core.Validator{
		Address:     fakeAddr,
		VotingPower: 100,
	}
	_, err = s.hostRegistry.GetHost(s.cctx.GoContext(), fakeVal)
	require.Error(t, err, "should error when getting host for non-existent validator")
}

func (s *IntegrationTestSuite) TestGetHostWithRegistration() {
	t := s.T()

	// Wait for at least one block to be produced before querying
	_, err := s.cctx.WaitForHeight(1)
	require.NoError(t, err, "failed to wait for first block")

	// Get the validator's operator address from the staking module
	// Since we only have one validator in testnode, we can just use that one
	stakingClient := stakingtypes.NewQueryClient(s.cctx.GRPCClient)
	validatorsResp, err := stakingClient.Validators(s.cctx.GoContext(), &stakingtypes.QueryValidatorsRequest{})
	require.NoError(t, err)
	require.NotEmpty(t, validatorsResp.Validators, "staking validators should not be empty")

	// In a single validator testnode, just use the first (and only) validator
	valOperatorAddr := validatorsResp.Validators[0].OperatorAddress
	t.Logf("Using validator operator address: %s for consensus address: %s", valOperatorAddr, s.validator.Address.String())

	// Create a TxClient to submit the transaction
	txClient, err := testnode.NewTxClientFromContext(s.cctx)
	require.NoError(t, err, "failed to create tx client")

	// Create and submit MsgSetFibreProviderInfo
	testHost := "validator.example.com:8080"
	msg := &types.MsgSetFibreProviderInfo{
		Signer: valOperatorAddr,
		Host:   testHost,
	}

	// Submit the transaction
	txResp, err := txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msg}, user.SetGasLimit(200000), user.SetFee(5000))
	require.NoError(t, err, "failed to submit transaction")
	require.Equal(t, uint32(0), txResp.Code, "transaction failed with code %d", txResp.Code)
	t.Logf("Transaction submitted successfully. TxHash: %s, Height: %d", txResp.TxHash, txResp.Height)

	host, err := s.hostRegistry.GetHost(s.cctx.GoContext(), s.validator)
	require.NoError(t, err, "GetHost should now succeed")
	require.NotEmpty(t, host.String())
	require.Equal(t, testHost, host.String(), "host should match what we registered")

	// Submit another transaction to update the host
	testHost2 := "validator.example.com:8081"
	msg = &types.MsgSetFibreProviderInfo{
		Signer: valOperatorAddr,
		Host:   testHost2,
	}

	txResp, err = txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msg}, user.SetGasLimit(200000), user.SetFee(5000))
	require.NoError(t, err, "failed to submit transaction")
	require.Equal(t, uint32(0), txResp.Code, "transaction failed with code %d", txResp.Code)
	t.Logf("Transaction submitted successfully. TxHash: %s, Height: %d", txResp.TxHash, txResp.Height)

	host, err = s.hostRegistry.GetHost(s.cctx.GoContext(), s.validator)
	require.NoError(t, err)
	require.NotEmpty(t, host.String())
	require.Equal(t, testHost, host.String(), "host should match what we registered")

	host, err = s.hostRegistry.PullHost(s.cctx.GoContext(), s.validator)
	require.NoError(t, err)
	require.NotEmpty(t, host.String())
	require.Equal(t, testHost2, host.String(), "host should match what we registered")
}

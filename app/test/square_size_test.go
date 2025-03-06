package app_test

import (
	"context"
	"fmt"
	"math/rand"
	"testing"
	"time"

	"cosmossdk.io/math"
	abci "github.com/cometbft/cometbft/abci/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	"github.com/celestiaorg/celestia-app/v4/test/txsim"
	"github.com/celestiaorg/celestia-app/v4/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v4/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/v4/x/blob/types"
)

func TestSquareSizeIntegrationTest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping square size integration test in short mode.")
	}
	suite.Run(t, new(SquareSizeIntegrationTest))
}

type SquareSizeIntegrationTest struct {
	suite.Suite

	cctx              testnode.Context
	rpcAddr, grpcAddr string
	enc               encoding.Config
}

func (s *SquareSizeIntegrationTest) SetupSuite() {
	t := s.T()
	t.Log("setting up square size integration test")

	s.enc = encoding.MakeTestConfig(app.ModuleEncodingRegisters...)
	cfg := testnode.DefaultConfig().WithModifiers(genesis.ImmediateProposals(s.enc.Codec)).WithTimeoutCommit(time.Second)

	cctx, rpcAddr, grpcAddr := testnode.NewNetwork(t, cfg)
	s.cctx = cctx
	s.rpcAddr = rpcAddr
	s.grpcAddr = grpcAddr

	err := s.cctx.WaitForNextBlock()
	require.NoError(t, err)
}

// TestSquareSizeUpperBound sets the app's params to specific sizes, then fills the
// block with spam txs to measure that the desired max is getting hit
func (s *SquareSizeIntegrationTest) TestSquareSizeUpperBound() {
	t := s.T()

	const waitBlocks = 10

	type test struct {
		name             string
		govMaxSquareSize int
		maxBytes         int
		expMaxSquareSize int
	}

	tests := []test{
		{
			name:             "default",
			govMaxSquareSize: appconsts.DefaultGovMaxSquareSize,
			maxBytes:         appconsts.DefaultMaxBytes,
			expMaxSquareSize: appconsts.DefaultGovMaxSquareSize,
		},
		{
			name:             "max bytes constrains square size",
			govMaxSquareSize: appconsts.DefaultGovMaxSquareSize,
			maxBytes:         appconsts.DefaultMaxBytes,
			expMaxSquareSize: appconsts.DefaultGovMaxSquareSize,
		},
		{
			name:             "gov square size == hardcoded max",
			govMaxSquareSize: appconsts.DefaultSquareSizeUpperBound,
			maxBytes:         appconsts.DefaultUpperBoundMaxBytes,
			expMaxSquareSize: appconsts.DefaultSquareSizeUpperBound,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	errCh := make(chan error)
	go func() {
		seqs := txsim.NewBlobSequence(txsim.NewRange(100_000, 100_000), txsim.NewRange(1, 1)).Clone(100)
		opts := txsim.DefaultOptions().WithSeed(rand.Int63()).WithPollTime(time.Second).SuppressLogs()
		errCh <- txsim.Run(ctx, s.grpcAddr, s.cctx.Keyring, s.enc, opts, seqs...)
	}()

	require.NoError(t, s.cctx.WaitForBlocks(2))

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s.SetupBlockSizeParams(t, tc.govMaxSquareSize, tc.maxBytes)
			require.NoError(t, s.cctx.WaitForBlocks(waitBlocks))

			// check that we're not going above the specified upper bound and that we hit the expected size
			latestHeight, err := s.cctx.LatestHeight()
			require.NoError(t, err)

			block, err := s.cctx.Client.Block(s.cctx.GoContext(), &latestHeight)
			require.NoError(t, err)
			require.LessOrEqual(t, block.Block.Data.SquareSize, uint64(tc.govMaxSquareSize))

			require.Equal(t, tc.expMaxSquareSize, int(block.Block.Data.SquareSize))
		})
	}
	cancel()
	err := <-errCh
	require.Contains(t, err.Error(), context.Canceled.Error())
}

// SetupBlockSizeParams will use the validator account to set the square size and
// max bytes parameters. It assumes that the governance params have been set to
// allow for fast acceptance of proposals, and will fail the test if the
// parameters are not set as expected.
func (s *SquareSizeIntegrationTest) SetupBlockSizeParams(t *testing.T, squareSize, maxBytes int) {
	// query existing x/consensus params and only update block max bytes
	consQueryClient := consensustypes.NewQueryClient(s.cctx.GRPCClient)
	consParamsResp, err := consQueryClient.Params(s.cctx.GoContext(), &consensustypes.QueryParamsRequest{})
	require.NoError(t, err)

	updatedParams := consParamsResp.Params
	updatedParams.Block.MaxBytes = int64(maxBytes)

	govAuthority := authtypes.NewModuleAddress("gov").String()
	msgUpdateConsensusParams := &consensustypes.MsgUpdateParams{
		Authority: govAuthority,
		Abci:      updatedParams.Abci,
		Block:     updatedParams.Block,
		Evidence:  updatedParams.Evidence,
		Validator: updatedParams.Validator,
	}

	// TODO: migrate x/blob to use self contained params, then use x/blob MsgUpdateParams, for now we use ExecLegacyContent
	maxSquareSizeParamChange := proposal.NewParamChange(blobtypes.ModuleName, string(blobtypes.KeyGovMaxSquareSize), fmt.Sprintf("\"%d\"", squareSize))
	content := proposal.NewParameterChangeProposal("x/blob max square size param", "param update proposal", []proposal.ParamChange{maxSquareSizeParamChange})

	contentAny, err := codectypes.NewAnyWithValue(content)
	require.NoError(t, err)

	msgExecLegacyContent := govv1.NewMsgExecLegacyContent(contentAny, govAuthority)

	proposerAddr := testfactory.GetAddress(s.cctx.Keyring, testnode.DefaultValidatorAccountName)
	msgSubmitProp, err := govv1.NewMsgSubmitProposal(
		[]sdk.Msg{msgExecLegacyContent, msgUpdateConsensusParams},
		sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000000))),
		proposerAddr.String(),
		"meta", "prop: update block size params", "summary", false,
	)
	require.NoError(t, err)

	txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.enc)
	require.NoError(t, err)

	res, err := txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msgSubmitProp}, blobfactory.DefaultTxOpts()...)
	require.NoError(t, err)

	txService := sdktx.NewServiceClient(s.cctx.GRPCClient)
	getTxResp, err := txService.GetTx(s.cctx.GoContext(), &sdktx.GetTxRequest{Hash: res.TxHash})
	require.NoError(t, err)
	require.Equal(t, res.Code, abci.CodeTypeOK, getTxResp.TxResponse.RawLog)

	require.NoError(t, s.cctx.WaitForNextBlock())

	// query the proposal to get the id
	govQueryClient := govv1.NewQueryClient(s.cctx.GRPCClient)
	propResp, err := govQueryClient.Proposals(s.cctx.GoContext(), &govv1.QueryProposalsRequest{ProposalStatus: govv1.StatusVotingPeriod})
	require.NoError(t, err)
	require.Len(t, propResp.Proposals, 1)

	// create and submit a new msgVote
	msgVote := govv1.NewMsgVote(testfactory.GetAddress(s.cctx.Keyring, testnode.DefaultValidatorAccountName), propResp.Proposals[0].Id, govv1.OptionYes, "")
	res, err = txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msgVote}, blobfactory.DefaultTxOpts()...)
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, res.Code)

	// wait for the voting period to complete
	require.NoError(t, s.cctx.WaitForBlocks(5))

	// check that the parameters were updated as expected
	blobQueryClient := blobtypes.NewQueryClient(s.cctx.GRPCClient)
	blobParamsResp, err := blobQueryClient.Params(s.cctx.GoContext(), &blobtypes.QueryParamsRequest{})
	require.NoError(t, err)
	require.Equal(t, uint64(squareSize), blobParamsResp.Params.GovMaxSquareSize)

	consParamsResp, err = consQueryClient.Params(s.cctx.GoContext(), &consensustypes.QueryParamsRequest{})
	require.NoError(t, err)
	require.Equal(t, int64(maxBytes), consParamsResp.Params.Block.MaxBytes)
}

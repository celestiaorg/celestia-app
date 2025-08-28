package app_test

import (
	"context"
	"math/rand"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v6/app"
	"github.com/celestiaorg/celestia-app/v6/app/encoding"
	"github.com/celestiaorg/celestia-app/v6/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v6/pkg/user"
	"github.com/celestiaorg/celestia-app/v6/test/txsim"
	"github.com/celestiaorg/celestia-app/v6/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v6/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v6/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/v6/x/blob/types"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	consensustypes "github.com/cosmos/cosmos-sdk/x/consensus/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
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

	s.enc = encoding.MakeConfig(app.ModuleEncodingRegisters...)
	cfg := testnode.DefaultConfig().
		WithModifiers(genesis.ImmediateProposals(s.enc.Codec)).
		WithTimeoutCommit(time.Millisecond * 500). // long timeout commit to provide time for submitting txs
		WithFundedAccounts("txsim")                // add a specific txsim account

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
			name:             "max bytes constrains square size",
			govMaxSquareSize: 64,
			maxBytes:         appconsts.DefaultMaxBytes,
			expMaxSquareSize: 64,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		// this for loop is a hack to restart txsim on failures
		for {
			select {
			case <-ctx.Done():
				return
			default:
				// submit blobs close to the max size
				seqs := txsim.NewBlobSequence(txsim.NewRange(100_000, mebibyte), txsim.NewRange(1, 1)).
					WithGasPrice(appconsts.DefaultMinGasPrice). // use a lower gas price than what is used below for the parameter changes
					Clone(10)
				opts := txsim.DefaultOptions().
					WithSeed(rand.Int63()).
					WithPollTime(time.Second).
					SpecifyMasterAccount("txsim"). // this attempts to avoid any conflicts with the param change txs
					UseFeeGrant().                 // feegrant isn't strictly required
					SuppressLogs()
				_ = txsim.Run(ctx, s.grpcAddr, s.cctx.Keyring, s.enc, opts, seqs...)

			}
		}
	}()

	require.NoError(t, s.cctx.WaitForBlocks(2))

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			s.SetupBlockSizeParams(t, tc.govMaxSquareSize, tc.maxBytes)
			require.NoError(t, s.cctx.WaitForBlocks(waitBlocks))

			squareSizes := make([]uint64, 0, waitBlocks+1)

			// check that we're not going above the specified upper bound and that we hit the expected size
			latestHeight, err := s.cctx.LatestHeight()
			require.NoError(t, err)

			for i := latestHeight - waitBlocks; i <= latestHeight; i++ {
				block, err := s.cctx.Client.Block(s.cctx.GoContext(), &latestHeight)
				require.NoError(t, err)
				require.LessOrEqual(t, block.Block.SquareSize, uint64(tc.govMaxSquareSize))
				squareSizes = append(squareSizes, block.Block.SquareSize)
			}

			// check that the expected max squareSize was hit at least once
			require.Contains(t, squareSizes, uint64(tc.expMaxSquareSize))
		})
	}
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

	newParams := blobtypes.DefaultParams()
	newParams.GovMaxSquareSize = uint64(squareSize)
	maxSquareSizeParamChange := blobtypes.NewMsgUpdateBlobParams(govAuthority, newParams)

	proposerAddr := testfactory.GetAddress(s.cctx.Keyring, testnode.DefaultValidatorAccountName)
	msgSubmitProp, err := govv1.NewMsgSubmitProposal(
		[]sdk.Msg{msgUpdateConsensusParams, maxSquareSizeParamChange},
		sdk.NewCoins(sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000000000))),
		proposerAddr.String(),
		"meta", "prop: update block size params", "summary", false,
	)
	require.NoError(t, err)

	txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.enc)
	require.NoError(t, err)

	// set a gas price higher than that of above to ensure param
	// change txs get included quickly
	opt := user.SetGasLimitAndGasPrice(2_000_000, .1)

	res, err := txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msgSubmitProp}, opt)
	require.NoError(t, err)

	res, err = txClient.ConfirmTx(s.cctx.GoContext(), res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), res.Code)

	txService := sdktx.NewServiceClient(s.cctx.GRPCClient)
	getTxResp, err := txService.GetTx(s.cctx.GoContext(), &sdktx.GetTxRequest{Hash: res.TxHash})
	require.NoError(t, err)
	require.Equal(t, res.Code, abci.CodeTypeOK, getTxResp.TxResponse.RawLog)

	require.NoError(t, s.cctx.WaitForNextBlock())

	proposalID := uint64(0)

	// try to query and vote on the proposal within the voting period
	govQueryClient := govv1.NewQueryClient(s.cctx.GRPCClient)
	for i := 0; i < 30; i++ {
		// query the proposal to get the id
		propResp, err := govQueryClient.Proposals(s.cctx.GoContext(), &govv1.QueryProposalsRequest{ProposalStatus: govv1.StatusVotingPeriod})
		require.NoError(t, err)

		if len(propResp.Proposals) < 1 {
			time.Sleep(time.Millisecond * 100)
			continue
		}

		proposalID = propResp.Proposals[0].Id

		// immediately try to vote while we know the proposal is still active
		msgVote := govv1.NewMsgVote(testfactory.GetAddress(s.cctx.Keyring, testnode.DefaultValidatorAccountName), proposalID, govv1.OptionYes, "")
		res, err = txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msgVote}, opt)

		// if we get an inactive proposal error, the voting period expired - retry finding a new active proposal
		if err != nil && res != nil && res.Code != abci.CodeTypeOK {
			time.Sleep(time.Millisecond * 100)
			continue
		}
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, res.Code)
		break
	}

	res, err = txClient.ConfirmTx(s.cctx.GoContext(), res.TxHash)
	require.NoError(t, err)
	require.Equal(t, uint32(0), res.Code)

	// try to query a few times until the voting period has passed
	for i := 0; i < 20; i++ {
		// check that the parameters were updated as expected
		blobQueryClient := blobtypes.NewQueryClient(s.cctx.GRPCClient)
		blobParamsResp, err := blobQueryClient.Params(s.cctx.GoContext(), &blobtypes.QueryParamsRequest{})
		require.NoError(t, err)
		if uint64(squareSize) != blobParamsResp.Params.GovMaxSquareSize {
			time.Sleep(time.Millisecond * 500)
			continue
		}
		require.Equal(t, uint64(squareSize), blobParamsResp.Params.GovMaxSquareSize)

		consParamsResp, err = consQueryClient.Params(s.cctx.GoContext(), &consensustypes.QueryParamsRequest{})
		require.NoError(t, err)
		require.Equal(t, int64(maxBytes), consParamsResp.Params.Block.MaxBytes)
		break
	}
}

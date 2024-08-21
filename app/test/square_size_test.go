package app_test

import (
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v3/pkg/user"
	"github.com/celestiaorg/celestia-app/v3/test/txsim"
	"github.com/celestiaorg/celestia-app/v3/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v3/test/util/sdkutil"
	"github.com/celestiaorg/celestia-app/v3/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/v3/x/blob/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdktx "github.com/cosmos/cosmos-sdk/types/tx"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	oldgov "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/rand"
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
	ecfg              encoding.Config
}

func (s *SquareSizeIntegrationTest) SetupSuite() {
	t := s.T()
	t.Log("setting up square size integration test")
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
	cfg := testnode.DefaultConfig().
		WithModifiers(genesis.ImmediateProposals(s.ecfg.Codec))

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
	const numBlocks = 10

	type test struct {
		name                  string
		govMaxSquareSize      int
		maxBytes              int
		expectedMaxSquareSize int
	}

	tests := []test{
		{
			name:                  "default",
			govMaxSquareSize:      appconsts.DefaultGovMaxSquareSize,
			maxBytes:              appconsts.DefaultMaxBytes,
			expectedMaxSquareSize: appconsts.DefaultGovMaxSquareSize,
		},
		{
			name:                  "max bytes constrains square size",
			govMaxSquareSize:      appconsts.DefaultGovMaxSquareSize,
			maxBytes:              appconsts.DefaultMaxBytes,
			expectedMaxSquareSize: appconsts.DefaultGovMaxSquareSize,
		},
		{
			name:                  "gov square size == hardcoded max",
			govMaxSquareSize:      appconsts.DefaultSquareSizeUpperBound,
			maxBytes:              appconsts.DefaultUpperBoundMaxBytes,
			expectedMaxSquareSize: appconsts.DefaultSquareSizeUpperBound,
		},
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error)
	go func() {
		seqs := txsim.NewBlobSequence(
			txsim.NewRange(100_000, 100_000),
			txsim.NewRange(1, 1),
		).Clone(100)
		err := txsim.Run(
			ctx,
			s.grpcAddr,
			s.cctx.Keyring,
			encoding.MakeConfig(app.ModuleEncodingRegisters...),
			txsim.DefaultOptions().
				WithSeed(rand.Int63()).
				WithPollTime(time.Second).
				SuppressLogs(),
			seqs...,
		)
		errCh <- err
	}()

	require.NoError(t, s.cctx.WaitForBlocks(2))

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s.setBlockSizeParams(t, tt.govMaxSquareSize, tt.maxBytes)
			require.NoError(t, s.cctx.WaitForBlocks(numBlocks))

			// check that we're not going above the specified size and that we hit the specified size
			actualMaxSize := 0
			end, err := s.cctx.LatestHeight()
			require.NoError(t, err)
			for i := end - numBlocks; i < end; i++ {
				block, err := s.cctx.Client.Block(s.cctx.GoContext(), &i)
				require.NoError(t, err)
				require.LessOrEqual(t, block.Block.Data.SquareSize, uint64(tt.govMaxSquareSize))

				if block.Block.Data.SquareSize > uint64(actualMaxSize) {
					actualMaxSize = int(block.Block.Data.SquareSize)
				}
			}

			require.Equal(t, tt.expectedMaxSquareSize, actualMaxSize)
		})
	}
	cancel()
	err := <-errCh
	require.Contains(t, err.Error(), context.Canceled.Error())
}

// setBlockSizeParams will use the validator account to set the square size and
// max bytes parameters. It assumes that the governance params have been set to
// allow for fast acceptance of proposals, and will fail the test if the
// parameters are not set as expected.
func (s *SquareSizeIntegrationTest) setBlockSizeParams(t *testing.T, squareSize, maxBytes int) {
	account := "validator"

	// create and submit a new param change proposal for both params
	change1 := sdkutil.GovMaxSquareSizeParamChange(squareSize)
	change2 := sdkutil.MaxBlockBytesParamChange(s.ecfg.Codec, maxBytes)

	content := proposal.NewParameterChangeProposal(
		"title",
		"description",
		[]proposal.ParamChange{change1, change2},
	)
	addr := testfactory.GetAddress(s.cctx.Keyring, account)

	msg, err := oldgov.NewMsgSubmitProposal(
		content,
		sdk.NewCoins(
			sdk.NewCoin(appconsts.BondDenom, sdk.NewInt(1000000000))),
		addr,
	)
	require.NoError(t, err)

	txClient, err := user.SetupTxClient(s.cctx.GoContext(), s.cctx.Keyring, s.cctx.GRPCClient, s.ecfg)
	require.NoError(t, err)

	res, err := txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{msg}, blobfactory.DefaultTxOpts()...)
	require.NoError(t, err)
	serviceClient := sdktx.NewServiceClient(s.cctx.GRPCClient)
	getTxResp, err := serviceClient.GetTx(s.cctx.GoContext(), &sdktx.GetTxRequest{Hash: res.TxHash})
	require.NoError(t, err)
	require.Equal(t, res.Code, abci.CodeTypeOK, getTxResp.TxResponse.RawLog)

	require.NoError(t, s.cctx.WaitForNextBlock())

	// query the proposal to get the id
	gqc := v1.NewQueryClient(s.cctx.GRPCClient)
	gresp, err := gqc.Proposals(s.cctx.GoContext(), &v1.QueryProposalsRequest{ProposalStatus: v1.ProposalStatus_PROPOSAL_STATUS_VOTING_PERIOD})
	require.NoError(t, err)
	require.Len(t, gresp.Proposals, 1)

	// create and submit a new vote
	vote := v1.NewMsgVote(testfactory.GetAddress(s.cctx.Keyring, account), gresp.Proposals[0].Id, v1.VoteOption_VOTE_OPTION_YES, "")
	res, err = txClient.SubmitTx(s.cctx.GoContext(), []sdk.Msg{vote}, blobfactory.DefaultTxOpts()...)
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, res.Code)

	// wait for the voting period to complete
	time.Sleep(time.Second * 6)

	// check that the parameters got updated as expected
	bqc := blobtypes.NewQueryClient(s.cctx.GRPCClient)
	presp, err := bqc.Params(s.cctx.GoContext(), &blobtypes.QueryParamsRequest{})
	require.NoError(t, err)
	require.Equal(t, uint64(squareSize), presp.Params.GovMaxSquareSize)
	latestHeight, err := s.cctx.LatestHeight()
	require.NoError(t, err)

	for i := 0; i < 10; i++ {
		cpresp, err := s.cctx.Client.ConsensusParams(s.cctx.GoContext(), &latestHeight)
		require.NoError(t, err)
		if err != nil || cpresp == nil {
			time.Sleep(time.Second)
			continue
		}
		require.Equal(t, int64(maxBytes), cpresp.ConsensusParams.Block.MaxBytes)
		break
	}
}

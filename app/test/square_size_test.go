package app_test

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/test/txsim"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
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

	accounts          []string
	cctx              testnode.Context
	rpcAddr, grpcAddr string
	ecfg              encoding.Config
}

func (s *SquareSizeIntegrationTest) SetupSuite() {
	t := s.T()
	t.Log("setting up square size integration test")
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := []string{}

	cparams := testnode.DefaultParams()
	cparams.Block.MaxBytes = appconsts.MaxShareCount * appconsts.ContinuationSparseShareContentSize

	cctx, rpcAddr, grpcAddr := testnode.NewNetwork(
		t,
		cparams,
		testnode.DefaultTendermintConfig(),
		testnode.DefaultAppConfig(),
		accounts,
		testnode.ImmediateProposals(s.ecfg.Codec), // pass param changes in 2 seconds
	)

	s.accounts = accounts
	s.cctx = cctx
	s.rpcAddr = rpcAddr
	s.grpcAddr = grpcAddr
	err := s.cctx.WaitForNextBlock()
	require.NoError(t, err)
}

// TestMaxSquareSize sets the app's params to specific sizes, then fills the
// block with spam txs to measure that the desired max is getting hit
func (s *SquareSizeIntegrationTest) TestMaxSquareSize() {
	t := s.T()

	type test struct {
		name                                   string
		govMaxSquareSize                       uint64
		maxBytes                               int64
		blobSize, blobsPerPFB, maxPFBsPerBlock int
	}

	tests := []test{
		{
			name:             "default",
			govMaxSquareSize: appconsts.DefaultGovMaxSquareSize,
			maxBytes:         appconsts.DefaultMaxBytes,
			// using many small blobs ensures that there is a lot of encoding
			// overhead and therefore full squares
			blobSize:        10_000,
			blobsPerPFB:     100,
			maxPFBsPerBlock: 10,
		},
		{
			name:             "gov square size == hardcoded max",
			govMaxSquareSize: appconsts.MaxSquareSize,
			maxBytes:         int64(appconsts.MaxShareCount * appconsts.ContinuationSparseShareContentSize),
			blobSize:         10_000,
			blobsPerPFB:      100,
			maxPFBsPerBlock:  12,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s.setBlockSizeParams(t, tt.govMaxSquareSize, tt.maxBytes)
			start, end := s.fillBlock(tt.blobSize, tt.blobsPerPFB, tt.maxPFBsPerBlock, time.Second*10)

			// check that we're not going above the specified size and that we hit the specified size
			hitMaxCounter := 0
			for i := start; i < end; i++ {
				block, err := s.cctx.Client.Block(s.cctx.GoContext(), &i)
				require.NoError(t, err)
				require.LessOrEqual(t, block.Block.Data.SquareSize, tt.govMaxSquareSize)

				if block.Block.Data.SquareSize == tt.govMaxSquareSize {
					hitMaxCounter++
				}
			}
			require.Greater(t, hitMaxCounter, 0)
		})
	}
}

// fillBlock runs txsim with blob sequences using the provided
// arguments. The start and end blocks are returned.
func (s *SquareSizeIntegrationTest) fillBlock(blobSize, blobsPerPFB, pfbsPerBlock int, period time.Duration) (start, end int64) {
	t := s.T()
	seqs := txsim.NewBlobSequence(
		txsim.NewRange(blobSize/2, blobSize),
		txsim.NewRange(blobsPerPFB/2, blobsPerPFB),
	).Clone(pfbsPerBlock)

	ctx, cancel := context.WithTimeout(context.Background(), period)
	defer cancel()

	startBlock, err := s.cctx.Client.Block(s.cctx.GoContext(), nil)
	require.NoError(t, err)

	_ = txsim.Run(
		ctx,
		[]string{s.rpcAddr},
		[]string{s.grpcAddr},
		s.cctx.Keyring,
		rand.Int63(),
		time.Second,
		seqs...,
	)

	endBlock, err := s.cctx.Client.Block(s.cctx.GoContext(), nil)
	require.NoError(t, err)

	return startBlock.Block.Height, endBlock.Block.Height
}

// setBlockSizeParams will use the validator account to set the square size and
// max bytes parameters. It assumes that the governance params have been set to
// allow for fast acceptance of proposals, and will fail the test if the
// parameters are not set as expected.
func (s *SquareSizeIntegrationTest) setBlockSizeParams(t *testing.T, squareSize uint64, maxBytes int64) {
	account := "validator"

	bparams := &abci.BlockParams{
		MaxBytes: maxBytes,
		MaxGas:   -1,
	}

	// create and submit a new param change proposal for both params
	change1 := proposal.NewParamChange(
		blobtypes.ModuleName,
		string(blobtypes.KeyGovMaxSquareSize),
		fmt.Sprintf("\"%d\"", squareSize),
	)
	change2 := proposal.NewParamChange(
		baseapp.Paramspace,
		string(baseapp.ParamStoreKeyBlockParams),
		string(s.cctx.Codec.MustMarshalJSON(bparams)),
	)
	content := proposal.NewParameterChangeProposal(
		"title",
		"description",
		[]proposal.ParamChange{change1, change2},
	)
	addr := getAddress(account, s.cctx.Keyring)

	msg, err := oldgov.NewMsgSubmitProposal(
		content,
		sdk.NewCoins(
			sdk.NewCoin(app.BondDenom, sdk.NewInt(1000000000))),
		addr,
	)
	require.NoError(t, err)

	res, err := testnode.SignAndBroadcastTx(s.ecfg, s.cctx.Context, account, msg)
	require.NoError(t, err)
	require.NoError(t, s.cctx.WaitForNextBlock())
	resp, err := testnode.QueryTx(s.cctx.Context, res.TxHash, false)
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, resp.TxResult.Code)

	// query the proposal to get the id
	gqc := v1.NewQueryClient(s.cctx.GRPCClient)
	gresp, err := gqc.Proposals(s.cctx.GoContext(), &v1.QueryProposalsRequest{ProposalStatus: v1.ProposalStatus_PROPOSAL_STATUS_VOTING_PERIOD})
	require.NoError(t, err)
	require.Len(t, gresp.Proposals, 1)

	// create and submit a new vote
	vote := v1.NewMsgVote(getAddress(account, s.cctx.Keyring), gresp.Proposals[0].Id, v1.VoteOption_VOTE_OPTION_YES, "")
	res, err = testnode.SignAndBroadcastTx(s.ecfg, s.cctx.Context, account, vote)
	require.NoError(t, err)
	require.NoError(t, s.cctx.WaitForNextBlock())

	resp, err = testnode.QueryTx(s.cctx.Context, res.TxHash, false)
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, resp.TxResult.Code)

	// wait for the voting period to complete
	time.Sleep(time.Second * 3)

	// check that the parameters got updated as expected
	bqc := blobtypes.NewQueryClient(s.cctx.GRPCClient)
	presp, err := bqc.Params(s.cctx.GoContext(), &blobtypes.QueryParamsRequest{})
	require.NoError(t, err)
	require.Equal(t, squareSize, presp.Params.GovMaxSquareSize)

	// unfortunately the rpc connection is very flakey with super fast block
	// times, so we have to retry many times.
	var newMaxBytes int64
	for i := 0; i < 20; i++ {
		cpresp, err := s.cctx.Client.ConsensusParams(s.cctx.GoContext(), nil)
		if err != nil || cpresp == nil {
			continue
		}
		newMaxBytes = cpresp.ConsensusParams.Block.MaxBytes
	}
	require.Equal(t, maxBytes, newMaxBytes)
}

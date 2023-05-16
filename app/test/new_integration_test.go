package app_test

import (
	"fmt"
	"sync"
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	oldgov "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/rand"
)

func TestIntegrationTest(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode.")
	}
	suite.Run(t, new(IntegrationTest))
}

type IntegrationTest struct {
	suite.Suite

	accounts          []string
	cctx              testnode.Context
	rpcAddr, grpcAddr string
	ecfg              encoding.Config

	mut            sync.Mutex
	accountCounter int
}

func (s *IntegrationTest) SetupSuite() {
	t := s.T()
	t.Log("setting up square size integration test")

	accounts := make([]string, 20)
	for i := 0; i < 20; i++ {
		accounts[i] = rand.Str(10)
	}

	cparams := testnode.DefaultParams()
	cparams.Block.MaxBytes = appconsts.MaxShareCount * appconsts.ContinuationSparseShareContentSize

	blobParams := blobtypes.Params{
		GasPerBlobByte:   appconsts.DefaultGasPerBlobByte,
		GovMaxSquareSize: appconsts.MaxSquareSize,
	}

	cctx, rpcAddr, grpcAddr := testnode.NewNetwork(
		t,
		cparams,
		testnode.DefaultTendermintConfig(),
		testnode.DefaultAppConfig(),
		accounts,
		testnode.SetBlobParams(s.cctx.Codec, blobParams),
		testnode.ImmediateProposals(s.cctx.Codec),
	)

	s.accounts = accounts
	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
	s.cctx = cctx
	s.rpcAddr = rpcAddr
	s.grpcAddr = grpcAddr
	err := s.cctx.WaitForNextBlock()
	require.NoError(t, err)
}

func (s *IntegrationTest) unusedAccount() string {
	s.mut.Lock()
	acc := s.accounts[s.accountCounter]
	s.accountCounter++
	s.mut.Unlock()
	return acc
}

func (s *IntegrationTest) TestMaxSquareSize() {
	t := s.T()
	s.setBlockSizeParams(t, 64, appconsts.DefaultMaxBytes)
}

func (s *IntegrationTest) setBlockSizeParams(t *testing.T, squareSize uint64, maxBytes int64) {
	account := s.unusedAccount()

	bparams := &abci.BlockParams{
		MaxBytes: maxBytes,
		MaxGas:   -1,
	}

	change1 := proposal.NewParamChange(blobtypes.ModuleName, string(blobtypes.KeyGovMaxSquareSize), "64")
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
	require.NoError(t, s.cctx.WaitForNextBlock())

	resp, err := testnode.QueryTx(s.cctx.Context, res.TxHash, false)
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, resp.TxResult.Code)

	bqc := blobtypes.NewQueryClient(s.cctx.GRPCClient)
	presp, err := bqc.Params(s.cctx.GoContext(), &blobtypes.QueryParamsRequest{})
	require.NoError(t, err)

	fmt.Println("new params------ ", presp.Params)
}

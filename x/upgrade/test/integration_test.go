package test

import (
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	v1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	"github.com/cosmos/cosmos-sdk/x/upgrade/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func TestUpgrade(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping x/upgrade SDK integration test in short mode.")
	}
	suite.Run(t, new(UpgradeTestSuite))
}

type UpgradeTestSuite struct {
	suite.Suite

	accounts []string
	cctx     testnode.Context
	ecfg     encoding.Config

	govModuleAddress string

	mut            sync.Mutex
	accountCounter int
}

// SetupSuite inits a standard chain, with the only exception being a
// dramatically lowered quorum and threshold to pass proposals
func (s *UpgradeTestSuite) SetupSuite() {
	t := s.T()

	s.ecfg = encoding.MakeConfig(app.ModuleBasics)

	// we create an arbitrary number of funded accounts
	accounts := make([]string, 3)
	for i := 0; i < len(accounts); i++ {
		accounts[i] = tmrand.Str(9)
	}

	tmCfg := testnode.DefaultTendermintConfig()
	tmCfg.Consensus.TimeoutCommit = 3 * time.Second

	cfg := testnode.DefaultConfig().
		WithAccounts(accounts).
		WithTendermintConfig(tmCfg).
		WithGenesisOptions(testnode.ImmediateProposals(s.ecfg.Codec))

	cctx, _, _ := testnode.NewNetwork(t, cfg)

	s.accounts = accounts
	s.cctx = cctx
	require.NoError(t, s.cctx.WaitForNextBlock())

	// Retrieve the gov module account via grpc
	aqc := authtypes.NewQueryClient(s.cctx.GRPCClient)
	resp, err := aqc.ModuleAccountByName(
		s.cctx.GoContext(), &authtypes.QueryModuleAccountByNameRequest{Name: "gov"},
	)
	s.Require().NoError(err)
	var acc authtypes.AccountI
	err = s.ecfg.InterfaceRegistry.UnpackAny(resp.Account, &acc)
	s.Require().NoError(err)

	// Set the gov module address
	s.govModuleAddress = acc.GetAddress().String()
}

func (s *UpgradeTestSuite) unusedAccount() string {
	s.mut.Lock()
	acc := s.accounts[s.accountCounter]
	s.accountCounter++
	s.mut.Unlock()
	return acc
}

// TestLegacyGovUpgradeFailure verifies that a transaction with a legacy
// software upgrade proposal fails to execute.
func (s *UpgradeTestSuite) TestLegacyGovUpgradeFailure() {
	t := s.T()

	dep := sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(1000000000000)))
	acc := s.unusedAccount()
	accAddr := getAddress(acc, s.cctx.Keyring)

	sftwr := types.NewSoftwareUpgradeProposal("v1", "Social Consensus", types.Plan{
		Name:   "v1",
		Height: 20,
		Info:   "rough social consensus",
	})

	msg, err := v1beta1.NewMsgSubmitProposal(sftwr, dep, accAddr)
	require.NoError(t, err)

	// submit the transaction and wait a block for it to be included
	res, err := testnode.SignAndBroadcastTx(s.ecfg, s.cctx.Context, acc, msg)
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, res.Code) // we're only submitting the tx, so we expect everything to work
	require.NoError(t, s.cctx.WaitForNextBlock())

	// compare the result after the tx has been executed.
	finalResult, err := testnode.QueryTx(s.cctx.Context, res.TxHash, false)
	require.NoError(t, err)
	require.NotNil(t, finalResult)
	assert.Contains(t, finalResult.TxResult.Log, "no handler exists for proposal type")
}

// TestNewGovUpgradeFailure verifies that a transaction with a
// MsgSoftwareUpgrade fails to execute.
func (s *UpgradeTestSuite) TestNewGovUpgradeFailure() {
	t := s.T()
	sss := types.MsgSoftwareUpgrade{
		Authority: s.govModuleAddress,
		Plan: types.Plan{
			Name:   "v1",
			Height: 20,
			Info:   "rough social consensus",
		},
	}
	dep := sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(1000000000000)))
	acc := s.unusedAccount()
	accAddr := getAddress(acc, s.cctx.Keyring)
	msg, err := v1.NewMsgSubmitProposal([]sdk.Msg{&sss}, dep, accAddr.String(), "")
	require.NoError(t, err)

	// submit the transaction and wait a block for it to be included
	res, err := testnode.SignAndBroadcastTx(s.ecfg, s.cctx.Context, acc, msg)
	require.NoError(t, err)
	require.Equal(t, abci.CodeTypeOK, res.Code) // we're only submitting the tx, so we expect everything to work
	require.NoError(t, s.cctx.WaitForNextBlock())

	// compare the result after the tx has been executed.
	finalResult, err := testnode.QueryTx(s.cctx.Context, res.TxHash, false)
	require.NoError(t, err)
	require.NotNil(t, finalResult)
	require.Contains(t, finalResult.TxResult.Log, "proposal message not recognized by router")
}

func getAddress(account string, kr keyring.Keyring) sdk.AccAddress {
	rec, err := kr.Key(account)
	if err != nil {
		panic(err)
	}
	addr, err := rec.GetAddress()
	if err != nil {
		panic(err)
	}
	return addr
}

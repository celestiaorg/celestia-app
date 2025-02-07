package signal_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"cosmossdk.io/math"
	tmrand "cosmossdk.io/math/unsafe"
	"cosmossdk.io/x/upgrade/types"
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v4/test/util"
	"github.com/celestiaorg/celestia-app/v4/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v4/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

func TestLegacyUpgrade(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping x/upgrade SDK integration test in short mode.")
	}
	suite.Run(t, new(LegacyUpgradeTestSuite))
}

// TestRemoval verifies that no handler exists for msg-based software upgrade
// proposals.
func TestRemoval(t *testing.T) {
	app, _ := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams())
	msgSoftwareUpgrade := types.MsgSoftwareUpgrade{}
	router := app.MsgServiceRouter()
	handler := router.Handler(&msgSoftwareUpgrade)
	require.Nil(t, handler)
}

type LegacyUpgradeTestSuite struct {
	suite.Suite

	accounts []string
	cctx     testnode.Context
	ecfg     moduletestutil.TestEncodingConfig

	govModuleAddress string

	mut            sync.Mutex
	accountCounter int
}

// SetupSuite inits a standard chain, with the only exception being a
// dramatically lowered quorum and threshold to pass proposals
func (s *LegacyUpgradeTestSuite) SetupSuite() {
	t := s.T()

	s.ecfg = moduletestutil.MakeTestEncodingConfig()

	// we create an arbitrary number of funded accounts
	accounts := make([]string, 3)
	for i := 0; i < len(accounts); i++ {
		accounts[i] = tmrand.Str(9)
	}

	cfg := testnode.DefaultConfig().
		WithFundedAccounts(accounts...).
		WithModifiers(genesis.ImmediateProposals(s.ecfg.Codec))

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
	var acc sdk.AccountI
	err = s.ecfg.InterfaceRegistry.UnpackAny(resp.Account, &acc)
	s.Require().NoError(err)

	// Set the gov module address
	s.govModuleAddress = acc.GetAddress().String()
}

func (s *LegacyUpgradeTestSuite) unusedAccount() string {
	s.mut.Lock()
	acc := s.accounts[s.accountCounter]
	s.accountCounter++
	s.mut.Unlock()
	return acc
}

// TestNewGovUpgradeFailure verifies that a transaction with a
// MsgSoftwareUpgrade fails to execute.
func (s *LegacyUpgradeTestSuite) TestNewGovUpgradeFailure() {
	t := s.T()
	sss := types.MsgSoftwareUpgrade{
		Authority: s.govModuleAddress,
		Plan: types.Plan{
			Name:   "v1",
			Height: 20,
			Info:   "rough social consensus",
		},
	}
	dep := sdk.NewCoins(sdk.NewCoin(app.BondDenom, math.NewInt(1000000000000)))
	acc := s.unusedAccount()
	accAddr := getAddress(acc, s.cctx.Keyring)
	msg, err := v1.NewMsgSubmitProposal([]sdk.Msg{&sss}, dep, accAddr.String(), "", "title", "summary", false)
	require.NoError(t, err)

	// submit the transaction and wait a block for it to be included
	txClient, err := testnode.NewTxClientFromContext(s.cctx)
	require.NoError(t, err)
	subCtx, cancel := context.WithTimeout(s.cctx.GoContext(), time.Minute)
	defer cancel()
	_, err = txClient.SubmitTx(subCtx, []sdk.Msg{msg}, blobfactory.DefaultTxOpts()...)
	// As the type is not registered, the message will fail with unable to resolve type URL
	require.Error(t, err)
	code := err.(*user.BroadcastTxError).Code
	require.EqualValues(t, 2, code, err.Error())
}

func (s *LegacyUpgradeTestSuite) TestIBCUpgradeFailure() {
	// TODO upgrade to gov v1

	// t := s.T()
	// plan := types.Plan{
	// 	Name:   "v2",
	// 	Height: 20,
	// 	Info:   "this should not pass",
	// }
	// upgradedClientState := &ibctmtypes.ClientState{}

	// upgradeMsg, err := ibctypes.NewUpgradeProposal("Upgrade to v2!", "Upgrade to v2!", plan, upgradedClientState)
	// require.NoError(t, err)

	// dep := sdk.NewCoins(sdk.NewCoin(app.BondDenom, math.NewInt(1000000000000)))
	// acc := s.unusedAccount()
	// accAddr := getAddress(acc, s.cctx.Keyring)
	// msg, err := v1beta1.NewMsgSubmitProposal(upgradeMsg, dep, accAddr)
	// require.NoError(t, err)

	// // submit the transaction and wait a block for it to be included
	// txClient, err := testnode.NewTxClientFromContext(s.cctx)
	// require.NoError(t, err)
	// subCtx, cancel := context.WithTimeout(s.cctx.GoContext(), time.Minute)
	// defer cancel()
	// _, err = txClient.SubmitTx(subCtx, []sdk.Msg{msg}, blobfactory.DefaultTxOpts()...)
	// require.Error(t, err)
	// code := err.(*user.ExecutionError).Code
	// require.EqualValues(t, 9, code) // we're only submitting the tx, so we expect everything to work
	// assert.Contains(t, err.Error(), "ibc upgrade proposal not supported")
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

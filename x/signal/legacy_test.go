package signal_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v3/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v3/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	v1beta1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	"github.com/cosmos/cosmos-sdk/x/upgrade/types"
	ibctypes "github.com/cosmos/ibc-go/v6/modules/core/02-client/types"
	ibctmtypes "github.com/cosmos/ibc-go/v6/modules/light-clients/07-tendermint/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	tmrand "github.com/tendermint/tendermint/libs/rand"
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
	ecfg     encoding.Config

	govModuleAddress string

	mut            sync.Mutex
	accountCounter int
}

// SetupSuite inits a standard chain, with the only exception being a
// dramatically lowered quorum and threshold to pass proposals
func (s *LegacyUpgradeTestSuite) SetupSuite() {
	t := s.T()

	s.ecfg = encoding.MakeConfig(app.ModuleBasics)

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
	var acc authtypes.AccountI
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

// TestLegacyGovUpgradeFailure verifies that a transaction with a legacy
// software upgrade proposal fails to execute.
func (s *LegacyUpgradeTestSuite) TestLegacyGovUpgradeFailure() {
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
	txClient, err := testnode.NewTxClientFromContext(s.cctx)
	require.NoError(t, err)
	subCtx, cancel := context.WithTimeout(s.cctx.GoContext(), time.Minute)
	defer cancel()
	_, err = txClient.SubmitTx(subCtx, []sdk.Msg{msg}, blobfactory.DefaultTxOpts()...)
	code := err.(*user.BroadcastTxError).Code
	errLog := err.(*user.BroadcastTxError).ErrorLog
	// As the type is not registered, the message will fail with unable to resolve type URL
	require.EqualValues(t, 2, code, errLog)
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
	dep := sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(1000000000000)))
	acc := s.unusedAccount()
	accAddr := getAddress(acc, s.cctx.Keyring)
	msg, err := v1.NewMsgSubmitProposal([]sdk.Msg{&sss}, dep, accAddr.String(), "")
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
	errLog := err.(*user.BroadcastTxError).ErrorLog
	require.EqualValues(t, 2, code, errLog)
}

func (s *LegacyUpgradeTestSuite) TestIBCUpgradeFailure() {
	t := s.T()
	plan := types.Plan{
		Name:   "v2",
		Height: 20,
		Info:   "this should not pass",
	}
	upgradedClientState := &ibctmtypes.ClientState{}

	upgradeMsg, err := ibctypes.NewUpgradeProposal("Upgrade to v2!", "Upgrade to v2!", plan, upgradedClientState)
	require.NoError(t, err)

	dep := sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(1000000000000)))
	acc := s.unusedAccount()
	accAddr := getAddress(acc, s.cctx.Keyring)
	msg, err := v1beta1.NewMsgSubmitProposal(upgradeMsg, dep, accAddr)
	require.NoError(t, err)

	// submit the transaction and wait a block for it to be included
	txClient, err := testnode.NewTxClientFromContext(s.cctx)
	require.NoError(t, err)
	subCtx, cancel := context.WithTimeout(s.cctx.GoContext(), time.Minute)
	defer cancel()
	_, err = txClient.SubmitTx(subCtx, []sdk.Msg{msg}, blobfactory.DefaultTxOpts()...)
	require.Error(t, err)
	code := err.(*user.ExecutionError).Code
	errLog := err.(*user.ExecutionError).ErrorLog
	require.EqualValues(t, 9, code) // we're only submitting the tx, so we expect everything to work
	assert.Contains(t, errLog, "ibc upgrade proposal not supported")
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

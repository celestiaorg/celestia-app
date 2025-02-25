package signal_test

import (
	"sync"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"

	tmrand "cosmossdk.io/math/unsafe"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v4/test/util/testnode"
)

func TestLegacyUpgrade(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping x/upgrade SDK integration test in short mode.")
	}
	suite.Run(t, new(LegacyUpgradeTestSuite))
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

	s.ecfg = encoding.MakeTestConfig(app.ModuleEncodingRegisters...)

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

func (s *LegacyUpgradeTestSuite) unusedAccount() string {
	s.mut.Lock()
	acc := s.accounts[s.accountCounter]
	s.accountCounter++
	s.mut.Unlock()
	return acc
}

// TODO: Finish test refactor. The x/circuit AnteHandler does not filter a MsgSubmitProposal's msgs.
// It is blocked by x/circuit after voting on proposal when its msgs are executed.
// func (s *LegacyUpgradeTestSuite) TestNewGovUpgradeFailure() {
// 	t := s.T()
// 	msgSoftwareUpgrade := &upgradetypes.MsgSoftwareUpgrade{
// 		Authority: s.govModuleAddress,
// 		Plan: upgradetypes.Plan{
// 			Name:   "v1",
// 			Height: 20,
// 			Info:   "rough social consensus",
// 		},
// 	}

// 	initialDeposit := sdk.NewCoins(sdk.NewCoin(app.BondDenom, math.NewInt(1000000000000)))
// 	proposerAcc := s.unusedAccount()
// 	proposerAddr := getAddress(proposerAcc, s.cctx.Keyring)

// 	msg, err := govv1.NewMsgSubmitProposal([]sdk.Msg{msgSoftwareUpgrade}, initialDeposit, proposerAddr.String(), "meta", "title", "summary", false)
// 	require.NoError(t, err)

// 	// submit the transaction and wait a block for it to be included
// 	txClient, err := testnode.NewTxClientFromContext(s.cctx)
// 	require.NoError(t, err)

// 	// TODO: why timeout?
// 	subCtx, cancel := context.WithTimeout(s.cctx.GoContext(), time.Minute)
// 	defer cancel()

// 	_, err = txClient.SubmitTx(subCtx, []sdk.Msg{msg}, blobfactory.DefaultTxOpts()...)
// 	// As the type is not registered, the message will fail with unable to resolve type URL
// 	require.Error(t, err)

// 	code := err.(*user.BroadcastTxError).Code
// 	require.EqualValues(t, 2, code, err.Error())
// }

// TODO: Finish test refactor. The x/circuit AnteHandler does not filter a MsgSubmitProposal's msgs.
// It is blocked by x/circuit after voting on proposal when its msgs are executed.
// func (s *LegacyUpgradeTestSuite) TestIBCUpgradeFailure() {
// 	t := s.T()
// 	plan := upgradetypes.Plan{
// 		Name:   "v2",
// 		Height: 20,
// 		Info:   "you shall not pass",
// 	}

// 	msgIBCSoftwareUpgrade, err := ibcclienttypes.NewMsgIBCSoftwareUpgrade(authtypes.NewModuleAddress("gov").String(), plan, &ibctm.ClientState{})
// 	require.NoError(t, err)

// 	initialDeposit := sdk.NewCoins(sdk.NewCoin(app.BondDenom, math.NewInt(1000000000000)))
// 	proposerAcc := s.unusedAccount()
// 	proposerAddr := getAddress(proposerAcc, s.cctx.Keyring)

// 	msgSubmitProposal, err := govv1.NewMsgSubmitProposal([]sdk.Msg{msgIBCSoftwareUpgrade}, initialDeposit, proposerAddr.String(), "meta", "ibc software upgrade", "summary", false)
// 	require.NoError(t, err)

// 	// submit the transaction and wait a block for it to be included
// 	txClient, err := testnode.NewTxClientFromContext(s.cctx)
// 	require.NoError(t, err)

// 	// TODO: why timeout?
// 	subCtx, cancel := context.WithTimeout(s.cctx.GoContext(), time.Minute)
// 	defer cancel()

// 	_, err = txClient.SubmitTx(subCtx, []sdk.Msg{msgSubmitProposal}, blobfactory.DefaultTxOpts()...)
// 	require.Error(t, err)

// 	code := err.(*user.ExecutionError).Code
// 	require.EqualValues(t, 9, code) // we're only submitting the tx, so we expect everything to work
// 	require.Contains(t, err.Error(), "ibc upgrade proposal not supported")
// }

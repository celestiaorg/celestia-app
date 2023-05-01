package qgb_test

import (
	"fmt"
	"math/big"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/testutil/testnode"
	"github.com/celestiaorg/celestia-app/x/qgb"
	qgbtypes "github.com/celestiaorg/celestia-app/x/qgb/types"
	v1 "github.com/celestiaorg/celestia-app/x/qgb/v1"
	"github.com/celestiaorg/celestia-app/x/qgb/v1beta1"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/testutil/mock"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/ethereum/go-ethereum/common"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	abci "github.com/tendermint/tendermint/abci/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func TestQGBVersionIntegrationTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping QGB version integration test in short mode.")
	}
	suite.Run(t, new(QGBVersionIntegrationTestSuite))
}

type QGBVersionIntegrationTestSuite struct {
	suite.Suite

	cleanup  func() error
	cctx     testnode.Context
	accounts []string
	ecfg     encoding.Config

	vm map[uint64]int64
}

func (s *QGBVersionIntegrationTestSuite) SetupSuite() {
	t := s.T()
	t.Log("setting up integration test suite")

	// setup random accounts
	for i := 0; i < 10; i++ {
		s.accounts = append(s.accounts, tmrand.Str(10))
	}

	s.ecfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)

	// set a custom version map
	vm := map[uint64]int64{
		0: 0,
		1: 20,
		2: 30,
	}
	s.vm = vm

	tmCfg := testnode.DefaultTendermintConfig()
	tmCfg.Consensus.TimeoutCommit = time.Millisecond * 400

	genState, kr, err := testnode.DefaultGenesisState(s.accounts...)
	require.NoError(t, err)

	tmNode, app, cctx, err := testnode.New(t, testnode.DefaultParams(), tmCfg, false, genState, kr, tmrand.Str(6), vm)
	require.NoError(t, err)

	cctx, stopNode, err := testnode.StartNode(tmNode, cctx)
	require.NoError(t, err)

	cctx, cleanupGRPC, err := testnode.StartGRPCServer(app, testnode.DefaultAppConfig(), cctx)
	require.NoError(t, err)

	cleanup := func() error {
		err := stopNode()
		if err != nil {
			return err
		}
		return cleanupGRPC()
	}

	s.cleanup = cleanup
	s.cctx = cctx

	s.Require().NoError(s.cctx.WaitForNextBlock())
}

func (s *QGBVersionIntegrationTestSuite) TearDownSuite() {
	t := s.T()
	t.Log("tearing down integration test suite")
	require.NoError(t, s.cleanup())
}

func (s *QGBVersionIntegrationTestSuite) TestVersionBump() {
	t := s.T()

	// wait until the app version should have changed
	h := int64(12)
	_, err := s.cctx.WaitForHeight(h)
	require.NoError(t, err)
	res, err := s.cctx.Client.Block(s.cctx.GoContext(), &h)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, v1beta1.SignificantPowerDifferenceThreshold, qgb.GetSignificantPowerDiffThreshold(res.Block.Header.Version.App))

	nonce1, err := queryLatestQGBNonce(s.cctx)
	require.NoError(t, err)

	// create a validator with 1% of the voting power (the first validtor has
	// 100 staked tokens) using v0, this will not cause a voting power change
	// large enough to trigger signing a new validator set hash for the qgb.
	// With the updated value in v1, it will.
	err = createValidator(s.cctx, s.ecfg, s.accounts[0], 1000000)
	require.NoError(t, err)

	require.NoError(t, s.cctx.WaitForNextBlock())

	nonce2, err := queryLatestQGBNonce(s.cctx)
	require.NoError(t, err)

	// the nonce should have not increased
	require.Equal(t, nonce1, nonce2)

	// wait until the app version should have changed
	h = int64(22)
	_, err = s.cctx.WaitForHeight(h)
	require.NoError(t, err)
	res, err = s.cctx.Client.Block(s.cctx.GoContext(), &h)
	require.NoError(t, err)
	require.NotNil(t, res)
	require.Equal(t, v1.SignificantPowerDifferenceThreshold, qgb.GetSignificantPowerDiffThreshold(res.Block.Header.Version.App))

	nonce3, err := queryLatestQGBNonce(s.cctx)
	require.NoError(t, err)

	// the nonce should have increased now that the threshold has been lowered
	require.Greater(t, nonce3, nonce2)
}

// createValidator creates a random validator with the given account name and delegation.
func createValidator(cctx testnode.Context, ecfg encoding.Config, account string, delegation int64) error {
	pv := mock.NewPV()

	valopAccAddr := getAddress(account, cctx.Keyring)
	valopAddr := sdk.ValAddress(valopAccAddr)
	evmAddr := common.BigToAddress(big.NewInt(420))

	msg, err := stakingtypes.NewMsgCreateValidator(
		valopAddr,
		pv.PrivKey.PubKey(),
		sdk.NewCoin(app.BondDenom, sdk.NewInt(delegation)),
		stakingtypes.NewDescription("taco tuesday", "my keybase", "www.celestia.org", "ping @celestiaorg on twitter", "fake validator"),
		stakingtypes.NewCommissionRates(sdk.NewDecWithPrec(6, 0o2), sdk.NewDecWithPrec(12, 0o2), sdk.NewDecWithPrec(1, 0o2)),
		sdk.NewInt(delegation),
		evmAddr,
	)
	if err != nil {
		return err
	}

	txres, err := testnode.SignAndBroadcastTx(ecfg, cctx.Context, account, msg)
	if err != nil {
		return err
	}
	if txres == nil {
		return fmt.Errorf("tx failed: nil")
	}
	if txres.Code != abci.CodeTypeOK {
		return fmt.Errorf("tx failed: %d", txres.Code)
	}
	return nil
}

func queryLatestQGBNonce(cctx testnode.Context) (uint64, error) {
	qgbQuerier := qgbtypes.NewQueryClient(cctx.GRPCClient)
	qgbRes, err := qgbQuerier.LatestAttestationNonce(cctx.GoContext(), &qgbtypes.QueryLatestAttestationNonceRequest{})
	if err != nil {
		return 0, err
	}
	return qgbRes.Nonce, nil
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

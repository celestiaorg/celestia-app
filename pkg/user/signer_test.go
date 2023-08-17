package user_test

import (
	"context"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/user"
	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
	"github.com/tendermint/tendermint/libs/rand"
)

func TestSignerTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping integration test in short mode.")
	}
	suite.Run(t, new(SignerTestSuite))
}

type SignerTestSuite struct {
	suite.Suite

	ctx    testnode.Context
	encCfg encoding.Config
	signer *user.Signer
}

func (s *SignerTestSuite) SetupSuite() {
	s.encCfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
	s.ctx, _, _ = testnode.NewNetwork(s.T(), testnode.DefaultConfig().WithAccounts([]string{"a"}))
	_, err := s.ctx.WaitForHeight(1)
	s.Require().NoError(err)
	rec, err := s.ctx.Keyring.Key("a")
	s.Require().NoError(err)
	addr, err := rec.GetAddress()
	s.Require().NoError(err)
	s.signer, err = user.SetupSigner(s.ctx.GoContext(), s.ctx.Keyring, s.ctx.GRPCClient, addr, s.encCfg)
	s.Require().NoError(err)
}

func (s *SignerTestSuite) TestSubmitPayForBlob() {
	t := s.T()
	blobs := blobfactory.ManyRandBlobs(t, rand.NewRand(), 1e3, 1e4)
	fee := user.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin(app.BondDenom, 1e6)))
	gas := user.SetGasLimit(1e6)
	resp, err := s.signer.SubmitPayForBlob(s.ctx.GoContext(), blobs, fee, gas)
	require.NoError(t, err)
	require.EqualValues(t, 0, resp.Code)
}

func (s *SignerTestSuite) TestSubmitTx() {
	t := s.T()
	fee := user.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin(app.BondDenom, 1e6)))
	gas := user.SetGasLimit(1e6)
	msg := bank.NewMsgSend(s.signer.Address().(sdk.AccAddress), testfactory.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(app.BondDenom, 10)))
	resp, err := s.signer.SubmitTx(s.ctx.GoContext(), []sdk.Msg{msg}, fee, gas)
	require.NoError(t, err)
	require.EqualValues(t, 0, resp.Code)
}

func (s *SignerTestSuite) ConfirmTxTimeout() {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	_, err := s.signer.ConfirmTx(ctx, string("E32BD15CAF57AF15D17B0D63CF4E63A9835DD1CEBB059C335C79586BC3013728"))
	require.Error(s.T(), err)
	require.Equal(s.T(), err, context.DeadlineExceeded)
}

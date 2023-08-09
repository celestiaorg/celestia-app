package user_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/user"
	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/test/util/testnode"
	sdk "github.com/cosmos/cosmos-sdk/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/rand"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestSubmitPayForBlob(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	ctx, _, grpcAddr := testnode.NewNetwork(t, testnode.DefaultConfig())
	conn, err := grpc.Dial(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	_, err = ctx.WaitForHeight(2)
	require.NoError(t, err)

	signer, err := user.SetupSingleSigner(ctx.GoContext(), ctx.Keyring, conn, encCfg)
	require.NoError(t, err)
	blobs := blobfactory.ManyRandBlobs(t, rand.NewRand(), 1e3, 1e4)
	fee := user.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin(app.BondDenom, 1e6)))
	gas := user.SetGasLimit(1e6)
	resp, err := signer.SubmitPayForBlob(ctx.GoContext(), blobs, fee, gas)
	require.NoError(t, err)
	require.EqualValues(t, 0, resp.Code)
}

func TestSubmitTx(t *testing.T) {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	ctx, _, grpcAddr := testnode.NewNetwork(t, testnode.DefaultConfig())
	conn, err := grpc.Dial(grpcAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)

	_, err = ctx.WaitForHeight(2)
	require.NoError(t, err)

	signer, err := user.SetupSingleSigner(ctx.GoContext(), ctx.Keyring, conn, encCfg)
	require.NoError(t, err)
	fee := user.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin(app.BondDenom, 1e6)))
	gas := user.SetGasLimit(1e6)
	msg := bank.NewMsgSend(signer.Address().(sdk.AccAddress), testfactory.RandomAddress().(sdk.AccAddress), sdk.NewCoins(sdk.NewInt64Coin(app.BondDenom, 10)))
	resp, err := signer.SubmitTx(ctx.GoContext(), []sdk.Msg{msg}, fee, gas)
	require.NoError(t, err)
	require.EqualValues(t, 0, resp.Code)
}

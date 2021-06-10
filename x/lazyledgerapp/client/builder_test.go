package client

import (
	"context"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/lazyledger/lazyledger-app/x/lazyledgerapp/types"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
)

func TestBuildSignedPayForMessage(t *testing.T) {
	testRing := generateKeyring(t, "testAccount")

	info, err := testRing.Key("testAccount")
	require.NoError(t, err)

	testAddr := info.GetAddress()

	b := NewBuilder(testAddr, "chain-id")
	require.NoError(t, err)

	namespace := []byte{1, 1, 1, 1, 1, 1, 1, 1}
	message := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 0}

	msg, err := types.NewMsgWirePayForMessage(
		namespace,
		message,
		info.GetPubKey().Bytes(),
		&types.TransactionFee{},
		4, 16, 32,
	)
	require.NoError(t, err)

	signedTx, err := b.BuildSignedTx(msg, testRing)
	require.NoError(t, err)

	sigs, err := signedTx.GetSignaturesV2()
	require.NoError(t, err)

	err = authsigning.VerifySignature(info.GetPubKey(), b.SignerData(), sigs[0].Data, b.encCfg.TxConfig.SignModeHandler(), signedTx)
	require.NoError(t, err)
}

func TestBroadcastPayForMessage(t *testing.T) {
	t.Skip("no local connection to app and no funds in wallet")
	testRing := generateKeyring(t, "testAccount")

	info, err := testRing.Key("testAccount")
	require.NoError(t, err)

	testAddr := info.GetAddress()

	b := NewBuilder(testAddr, "chain-id")
	require.NoError(t, err)

	RPCAddress := "127.0.0.1:9090"

	rpcClient, err := grpc.Dial(RPCAddress, grpc.WithInsecure())
	require.NoError(t, err)
	err = b.UpdateAccountNumber(context.TODO(), rpcClient)
	require.NoError(t, err)

	b.SetGasLimit(100000)

	coin := sdktypes.Coin{
		Denom:  "token",
		Amount: sdktypes.NewInt(10),
	}
	b.SetFeeAmount(sdktypes.NewCoins(coin))

	namespace := []byte{1, 1, 1, 1, 1, 1, 1, 1}
	message := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 0}

	msg, err := types.NewMsgWirePayForMessage(
		namespace,
		message,
		info.GetPubKey().Bytes(),
		&types.TransactionFee{},
		4, 16, 32,
	)
	require.NoError(t, err)

	signedTx, err := b.BuildSignedTx(msg, testRing)
	require.NoError(t, err)

	encodedTx, err := b.EncodeTx(signedTx)
	require.NoError(t, err)

	resp, err := BroadcastTx(context.TODO(), rpcClient, tx.BroadcastMode_BROADCAST_MODE_BLOCK, encodedTx)
	require.NoError(t, err)

	require.Equal(t, "", resp.TxResponse.Data)
}

func TestUpdateAccountNumber(t *testing.T) {
	t.Skip("no local connection to app and no funds in wallet")
	testRing := generateKeyring(t, "testAccount")

	info, err := testRing.Key("testAccount")
	require.NoError(t, err)

	testAddr := info.GetAddress()

	b := NewBuilder(testAddr, "test")
	require.NoError(t, err)

	RPCAddress := "127.0.0.1:9090"

	rpcClient, err := grpc.Dial(RPCAddress, grpc.WithInsecure())
	require.NoError(t, err)
	err = b.UpdateAccountNumber(context.TODO(), rpcClient)
	require.NoError(t, err)
}

func generateKeyring(t *testing.T, accts ...string) keyring.Keyring {
	t.Helper()
	kb := keyring.NewInMemory()

	for _, acc := range accts {
		_, _, err := kb.NewMnemonic(acc, keyring.English, "", hd.Secp256k1)
		if err != nil {
			t.Error(err)
		}
	}

	_, err := kb.NewAccount(testAccName, testMnemo, "1234", "", hd.Secp256k1)
	if err != nil {
		panic(err)
	}

	return kb
}

const (
	// nolint:lll
	testMnemo   = `ramp soldier connect gadget domain mutual staff unusual first midnight iron good deputy wage vehicle mutual spike unlock rocket delay hundred script tumble choose`
	testAccName = "test-account"
)

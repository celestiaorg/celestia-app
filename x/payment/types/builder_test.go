package types

import (
	"context"
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/stretchr/testify/require"
	coretypes "github.com/tendermint/tendermint/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestBuildWirePayForData(t *testing.T) {
	testRing := generateKeyring(t)

	info, err := testRing.Key(testAccName)
	require.NoError(t, err)

	k := NewKeyringSigner(testRing, testAccName, "chain-id")
	require.NoError(t, err)

	namespace := []byte{1, 1, 1, 1, 1, 1, 1, 1}
	message := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 0}

	msg, err := NewWirePayForData(namespace, message, 4, 16, 32)
	require.NoError(t, err)

	signedTx, err := k.BuildSignedTx(k.NewTxBuilder(), msg)
	require.NoError(t, err)

	rawTx, err := makePaymentEncodingConfig().TxConfig.TxEncoder()(signedTx)
	require.NoError(t, err)

	_, isMalleated := coretypes.UnwrapMalleatedTx(rawTx)
	require.False(t, isMalleated)

	sigs, err := signedTx.GetSignaturesV2()
	require.NoError(t, err)

	signerData := authsigning.SignerData{
		ChainID:       k.chainID,
		AccountNumber: k.accountNumber,
		Sequence:      k.sequence,
	}

	pub, err := info.GetPubKey()
	require.NoError(t, err)

	err = authsigning.VerifySignature(pub, signerData, sigs[0].Data, k.encCfg.TxConfig.SignModeHandler(), signedTx)
	require.NoError(t, err)
}

func TestBroadcastPayForData(t *testing.T) {
	testRing := generateKeyring(t)
	info, err := testRing.Key(testAccName)
	require.NoError(t, err)
	addr, err := info.GetAddress()
	require.NoError(t, err)
	t.Skipf("no local connection to app and no funds in wallet %s", addr)

	k := NewKeyringSigner(testRing, testAccName, "test")

	RPCAddress := "127.0.0.1:9090"

	rpcClient, err := grpc.Dial(RPCAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	err = k.QueryAccountNumber(context.TODO(), rpcClient)
	require.NoError(t, err)

	builder := k.NewTxBuilder()

	builder.SetGasLimit(100000)

	coin := sdktypes.Coin{
		Denom:  "token",
		Amount: sdktypes.NewInt(10),
	}
	builder.SetFeeAmount(sdktypes.NewCoins(coin))

	namespace := []byte{1, 1, 1, 1, 1, 1, 1, 1}
	message := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 0}

	msg, err := NewWirePayForData(namespace, message, 4, 16, 32)
	require.NoError(t, err)

	signedTx, err := k.BuildSignedTx(builder, msg)
	require.NoError(t, err)

	encodedTx, err := k.EncodeTx(signedTx)
	require.NoError(t, err)

	resp, err := BroadcastTx(context.TODO(), rpcClient, tx.BroadcastMode_BROADCAST_MODE_BLOCK, encodedTx)
	require.NoError(t, err)

	require.Equal(t, "", resp.TxResponse.Data)
}

func TestQueryAccountNumber(t *testing.T) {
	t.Skip("no local connection to app and no funds in wallet")
	testRing := generateKeyring(t)

	k := NewKeyringSigner(testRing, testAccName, "test")

	RPCAddress := "127.0.0.1:9090"

	rpcClient, err := grpc.Dial(RPCAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	err = k.QueryAccountNumber(context.TODO(), rpcClient)
	require.NoError(t, err)
}

func generateKeyring(t *testing.T, accts ...string) keyring.Keyring {
	t.Helper()
	encCfg := makePaymentEncodingConfig()
	kb := keyring.NewInMemory(encCfg.Codec)

	for _, acc := range accts {
		_, _, err := kb.NewMnemonic(acc, keyring.English, "", "", hd.Secp256k1)
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

func generateKeyringSigner(t *testing.T, accts ...string) *KeyringSigner {
	kr := generateKeyring(t, accts...)
	return NewKeyringSigner(kr, testAccName, testChainID)
}

const (
	// nolint:lll
	testMnemo   = `ramp soldier connect gadget domain mutual staff unusual first midnight iron good deputy wage vehicle mutual spike unlock rocket delay hundred script tumble choose`
	testAccName = "test-account"
	testChainID = "test-chain-1"
)

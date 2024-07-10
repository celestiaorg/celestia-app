package types

import (
	"context"
	"testing"

	sdktypes "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/stretchr/testify/require"
	coretypes "github.com/tendermint/tendermint/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

func TestBuildPayForBlob(t *testing.T) {
	testRing := GenerateKeyring(t)

	info, err := testRing.Key(TestAccName)
	require.NoError(t, err)

	k := NewKeyringSigner(testRing, TestAccName, testChainID)
	require.NoError(t, err)

	msg := validMsgPayForBlobs(t)

	signedTx, err := k.BuildSignedTx(k.NewTxBuilder(), msg)
	require.NoError(t, err)

	rawTx, err := makeBlobEncodingConfig().TxConfig.TxEncoder()(signedTx)
	require.NoError(t, err)

	_, isIndexWrapper := coretypes.UnmarshalIndexWrapper(rawTx)
	require.False(t, isIndexWrapper)

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

func TestBroadcastPayForBlob(t *testing.T) {
	testRing := GenerateKeyring(t)
	info, err := testRing.Key(TestAccName)
	require.NoError(t, err)
	addr, err := info.GetAddress()
	require.NoError(t, err)
	t.Skipf("no local connection to app and no funds in wallet %s", addr)

	k := NewKeyringSigner(testRing, TestAccName, testChainID)

	RPCAddress := "127.0.0.1:9090"

	rpcClient, err := grpc.NewClient(RPCAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
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

	msg := validMsgPayForBlobs(t)

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
	k := GenerateKeyringSigner(t, TestAccName)

	RPCAddress := "127.0.0.1:9090"

	rpcClient, err := grpc.NewClient(RPCAddress, grpc.WithTransportCredentials(insecure.NewCredentials()))
	require.NoError(t, err)
	err = k.QueryAccountNumber(context.TODO(), rpcClient)
	require.NoError(t, err)
}

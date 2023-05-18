package types_test

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/namespace"
	appns "github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/celestiaorg/celestia-app/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/x/blob/types"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/cosmos/cosmos-sdk/x/authz"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	"github.com/tendermint/tendermint/pkg/consts"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

const (
	denom = "utia"
)

func TestNewBlob(t *testing.T) {
	rawBlob := []byte{1}
	validBlob, err := types.NewBlob(namespace.RandomBlobNamespace(), rawBlob, appconsts.ShareVersionZero)
	require.NoError(t, err)
	require.Equal(t, validBlob.Data, rawBlob)

	_, err = types.NewBlob(appns.TxNamespace, rawBlob, appconsts.ShareVersionZero)
	require.Error(t, err)

	_, err = types.NewBlob(namespace.RandomBlobNamespace(), []byte{}, appconsts.ShareVersionZero)
	require.Error(t, err)
}

func TestVerifySignature(t *testing.T) {
	_, addr, signer, encCfg := setupSigTest(t)
	coin := sdk.Coin{
		Denom:  denom,
		Amount: sdk.NewInt(10),
	}

	opts := []types.TxBuilderOption{
		types.SetFeeAmount(sdk.NewCoins(coin)),
		types.SetGasLimit(10000000),
	}

	msg, blob := randMsgPayForBlobsWithNamespaceAndSigner(
		t,
		addr.String(),
		namespace.RandomBlobNamespace(),
		100,
	)
	builder := signer.NewTxBuilder(opts...)
	stx, err := signer.BuildSignedTx(builder, msg)
	require.NoError(t, err)

	rawTx, err := encCfg.TxConfig.TxEncoder()(stx)
	require.NoError(t, err)

	cTx, err := coretypes.MarshalBlobTx(rawTx, blob)
	require.NoError(t, err)

	uTx, isBlob := coretypes.UnmarshalBlobTx(cTx)
	require.True(t, isBlob)

	wTx, err := coretypes.MarshalIndexWrapper(uTx.Tx, 100)
	require.NoError(t, err)

	uwTx, isMal := coretypes.UnmarshalIndexWrapper(wTx)
	require.True(t, isMal)

	sTx, err := encCfg.TxConfig.TxDecoder()(uwTx.Tx)
	require.NoError(t, err)

	sigTx, ok := sTx.(authsigning.SigVerifiableTx)
	require.True(t, ok)

	sigs, err := sigTx.GetSignaturesV2()
	require.NoError(t, err)
	require.Equal(t, 1, len(sigs))
	sig := sigs[0]

	// verify the signatures of the prepared txs
	sdata, err := signer.GetSignerData()
	require.NoError(t, err)

	err = authsigning.VerifySignature(
		sdata.PubKey,
		sdata,
		sig.Data,
		encCfg.TxConfig.SignModeHandler(),
		sTx,
	)
	assert.NoError(t, err)
}

func setupSigTest(t *testing.T) (string, sdk.Address, *types.KeyringSigner, encoding.Config) {
	acc := "test account"
	signer := types.GenerateKeyringSigner(t, acc)
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	addr, err := signer.GetSignerInfo().GetAddress()
	require.NoError(t, err)
	return acc, addr, signer, encCfg
}

func randMsgPayForBlobsWithNamespaceAndSigner(t *testing.T, signer string, ns appns.Namespace, size int) (*types.MsgPayForBlobs, *tmproto.Blob) {
	blob, err := types.NewBlob(ns, tmrand.Bytes(size), appconsts.ShareVersionZero)
	require.NoError(t, err)
	msg, err := types.NewMsgPayForBlobs(
		signer,
		blob,
	)
	if err != nil {
		panic(err)
	}
	return msg, blob
}

func TestValidateBlobTxUsingMsgExec(t *testing.T) {
	grantee := testfactory.RandomAddress()
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	pfb, blob := blobfactory.RandMsgPayForBlobs(1024)
	blob2 := types.BlobToProto(testfactory.GenerateRandomBlob(512))
	msg := authz.NewMsgExec(grantee, []sdk.Msg{pfb})
	invalidMsgExec := authz.NewMsgExec(sdk.AccAddress{}, []sdk.Msg{pfb})
	require.Error(t, invalidMsgExec.ValidateBasic())
	bankMsg := bank.NewMsgSend(testfactory.RandomAddress(), testfactory.RandomAddress(), sdk.NewCoins(sdk.NewCoin("foo", sdk.NewInt(10))))
	msgExecWithTwoMsgs := authz.NewMsgExec(grantee, []sdk.Msg{pfb, bankMsg})

	testCases := []struct {
		name   string
		msgs   []sdk.Msg
		blobs  []*tmproto.Blob
		expErr bool
	}{
		{
			name:   "valid blob tx with msg exec",
			msgs:   []sdk.Msg{&msg},
			blobs:  []*tmproto.Blob{blob},
			expErr: false,
		},
		{
			name:   "blob tx using msg exec with unaccounted blob",
			msgs:   []sdk.Msg{&msg},
			blobs:  []*tmproto.Blob{blob, blob2},
			expErr: true,
		},
		{
			name:   "blob tx using invalid msg exec",
			msgs:   []sdk.Msg{&invalidMsgExec},
			blobs:  []*tmproto.Blob{blob},
			expErr: true,
		},
		{
			name:   "blob tx using invalid msg exec that has multiple messages",
			msgs:   []sdk.Msg{&msgExecWithTwoMsgs},
			blobs:  []*tmproto.Blob{blob},
			expErr: true,
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			builder := encCfg.TxConfig.NewTxBuilder()
			require.NoError(t, builder.SetMsgs(tc.msgs...))
			tx := builder.GetTx()
			txBytes, err := encCfg.TxConfig.TxEncoder()(tx)
			require.NoError(t, err)
			blobTx := tmproto.BlobTx{
				Tx:     txBytes,
				Blobs:  tc.blobs,
				TypeId: consts.ProtoBlobTxTypeID,
			}
			if tc.expErr {
				require.Error(t, types.ValidateBlobTx(encCfg.TxConfig, blobTx))
			} else {
				require.NoError(t, types.ValidateBlobTx(encCfg.TxConfig, blobTx))
			}
		})
	}
}

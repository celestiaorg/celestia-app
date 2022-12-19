package types

import (
	"testing"

	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/testutil/namespace"
	tmrand "github.com/tendermint/tendermint/libs/rand"

	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	coretypes "github.com/tendermint/tendermint/types"
)

const (
	denom = "utia"
)

func TestNewBlob(t *testing.T) {
	rawBlob := []byte{1}
	validBlob, err := NewBlob([]byte{1, 2, 3, 4, 5, 6, 7, 8}, rawBlob)
	require.NoError(t, err)
	require.Equal(t, validBlob.Data, rawBlob)

	_, err = NewBlob(appconsts.TxNamespaceID, rawBlob)
	require.Error(t, err)

	_, err = NewBlob([]byte{1, 2, 3, 4, 5, 6, 7, 8}, []byte{})
	require.Error(t, err)
}

func TestVerifySignature(t *testing.T) {
	_, addr, signer, encCfg := setupSigTest(t)
	coin := sdk.Coin{
		Denom:  denom,
		Amount: sdk.NewInt(10),
	}

	opts := []TxBuilderOption{
		SetFeeAmount(sdk.NewCoins(coin)),
		SetGasLimit(10000000),
	}

	msg, blob := randMsgPayForBlobWithNamespaceAndSigner(
		addr.String(),
		namespace.RandomBlobNamespace(),
		100,
	)
	builder := signer.NewTxBuilder(opts...)
	stx, err := signer.BuildSignedTx(builder, msg)
	require.NoError(t, err)

	rawTx, err := encCfg.TxConfig.TxEncoder()(stx)
	require.NoError(t, err)

	wblob, err := NewBlob(msg.NamespaceId, blob)
	require.NoError(t, err)

	cTx, err := coretypes.MarshalBlobTx(rawTx, wblob)
	require.NoError(t, err)

	uTx, isBlob := coretypes.UnmarshalBlobTx(cTx)
	require.True(t, isBlob)

	wTx, err := coretypes.MarshalIndexWrapper(100, uTx.Tx)
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

func setupSigTest(t *testing.T) (string, sdk.Address, *KeyringSigner, encoding.Config) {
	acc := "test account"
	signer := GenerateKeyringSigner(t, acc)
	encCfg := makeBlobEncodingConfig()
	addr, err := signer.GetSignerInfo().GetAddress()
	require.NoError(t, err)
	return acc, addr, signer, encCfg
}

func randMsgPayForBlobWithNamespaceAndSigner(signer string, nid []byte, size int) (*MsgPayForBlob, []byte) {
	blob := tmrand.Bytes(size)
	msg, err := NewMsgPayForBlob(
		signer,
		nid,
		blob,
	)
	if err != nil {
		panic(err)
	}
	return msg, blob
}

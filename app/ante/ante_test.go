package ante_test

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/go-square/v2/share"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v4/test/util"
	"github.com/celestiaorg/celestia-app/v4/x/blob/types"
)

// Reproduces https://github.com/celestiaorg/celestia-app/issues/4847
func TestSigVerificationDecorator(t *testing.T) {
	testApp, _, _ := testutil.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	encodingConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	signModeHandler := encodingConfig.TxConfig.SignModeHandler()
	decorator := ante.NewSigVerificationDecorator(testApp.AccountKeeper, signModeHandler)

	require.Panics(t, func() {
		tx := getTx(t)
		simulate := false
		_, err := decorator.AnteHandle(sdk.Context{}, tx, simulate, nextAnteHandler)
		require.NoError(t, err)
	})
}

func getTx(t *testing.T) authsigning.Tx {
	namespace, err := share.NewV0Namespace([]byte("CeroA"))
	require.NoError(t, err)

	blob, err := share.NewV0Blob(namespace, []byte("data"))
	require.NoError(t, err)

	signer := "celestia1rky9086t340m7rmkctuj4spxwv2gc62vlwx59v"

	msg, err := types.NewMsgPayForBlobs(signer, appconsts.LatestVersion, blob)
	require.NoError(t, err)

	config := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	txBuilder := config.TxConfig.NewTxBuilder()

	err = txBuilder.SetMsgs(msg)
	require.NoError(t, err)

	signature := signing.SignatureV2{
		PubKey: nil, // This will cause the nil pointer dereference
		Data: &signing.SingleSignatureData{
			SignMode: signing.SignMode_SIGN_MODE_DIRECT,
		},
		Sequence: 0,
	}
	err = txBuilder.SetSignatures(signature)
	require.NoError(t, err)

	return txBuilder.GetTx()
}

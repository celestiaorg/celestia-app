package ante_test

import (
	"fmt"
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

	ctx := testApp.BaseApp.NewContext(false)
	accounts := testApp.AccountKeeper.GetAllAccounts(ctx)
	require.NotEmpty(t, accounts)

	account, err := getAccountWithPubKey(accounts)
	require.NoError(t, err)

	fmt.Printf("account: %s\n", account.GetAddress().String())
	fmt.Printf("pubKey: %s\n", account.GetPubKey().String())

	encodingConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	signModeHandler := encodingConfig.TxConfig.SignModeHandler()
	decorator := ante.NewSigVerificationDecorator(testApp.AccountKeeper, signModeHandler)

	tx := getTx(t, account)
	simulate := false

	require.Panics(t, func() {
		_, err = decorator.AnteHandle(ctx, tx, simulate, nextAnteHandler) // this panics due to the nil pubkey
		require.Error(t, err)
		require.ErrorContains(t, err, "signature verification failed")
	})
}

func getTx(t *testing.T, account sdk.AccountI) authsigning.Tx {
	namespace, err := share.NewV0Namespace([]byte("CeroA"))
	require.NoError(t, err)

	blob, err := share.NewV0Blob(namespace, []byte("data"))
	require.NoError(t, err)

	signer := account.GetAddress().String()
	msg, err := types.NewMsgPayForBlobs(signer, appconsts.LatestVersion, blob)
	require.NoError(t, err)

	config := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	txBuilder := config.TxConfig.NewTxBuilder()

	err = txBuilder.SetMsgs(msg)
	require.NoError(t, err)

	signature := signing.SignatureV2{
		PubKey: nil,
		Data: &signing.SingleSignatureData{
			SignMode: signing.SignMode_SIGN_MODE_DIRECT,
		},
		Sequence: 0,
	}
	err = txBuilder.SetSignatures(signature)
	require.NoError(t, err)

	return txBuilder.GetTx()
}

func getAccountWithPubKey(accounts []sdk.AccountI) (sdk.AccountI, error) {
	for _, account := range accounts {
		if account.GetPubKey() != nil {
			return account, nil
		}
	}
	return nil, fmt.Errorf("no account found with a pubkey")
}

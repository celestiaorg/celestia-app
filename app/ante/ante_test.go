package ante_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/go-square/v2/share"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/ante"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v4/test/util"
	"github.com/celestiaorg/celestia-app/v4/x/blob/types"
)

// Reproduces https://github.com/celestiaorg/celestia-app/issues/4847
func TestSafeSigVerificationDecorator(t *testing.T) {
	testApp, _, _ := testutil.NewTestAppWithGenesisSet(app.DefaultConsensusParams())

	ctx := testApp.NewContext(false)
	accounts := testApp.AccountKeeper.GetAllAccounts(ctx)
	require.NotEmpty(t, accounts)

	account, err := getAccountWithPubKey(accounts)
	require.NoError(t, err)

	fmt.Printf("account: %s\n", account.GetAddress().String())
	fmt.Printf("pubKey: %s\n", account.GetPubKey().String())

	encodingConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	signModeHandler := encodingConfig.TxConfig.SignModeHandler()
	decorator := ante.NewSafeSigVerificationDecorator(testApp.AccountKeeper, signModeHandler)
	simulate := false

	t.Run("rejects transaction with nil PubKey", func(t *testing.T) {
		tx := createTxWithNilPubKey(t, account)

		// Should now return an error instead of panicking
		_, err := decorator.AnteHandle(ctx, tx, simulate, nextAnteHandler)
		require.Error(t, err)
		require.Contains(t, err.Error(), "nil PubKey")
		fmt.Printf("Successfully caught error: %v\n", err)
	})

	t.Run("accepts transaction with valid PubKey", func(t *testing.T) {
		tx := createTxWithValidPubKey(t, account)

		// Should pass through to the standard decorator
		// Note: This may still fail signature verification but shouldn't panic
		_, err := decorator.AnteHandle(ctx, tx, simulate, nextAnteHandler)
		// We expect some error since we didn't properly sign the transaction,
		// but it should not be a nil PubKey error
		if err != nil {
			require.NotContains(t, err.Error(), "nil PubKey")
		}
	})
}

func getAccountWithPubKey(accounts []sdk.AccountI) (sdk.AccountI, error) {
	for _, account := range accounts {
		if account.GetPubKey() != nil {
			return account, nil
		}
	}
	return nil, fmt.Errorf("no account found with a pubkey")
}

func createTxWithNilPubKey(t *testing.T, account sdk.AccountI) authsigning.Tx {
	config := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	txBuilder := config.TxConfig.NewTxBuilder()

	msg := getMsg(t, account)
	err := txBuilder.SetMsgs(msg)
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

func createTxWithValidPubKey(t *testing.T, account sdk.AccountI) authsigning.Tx {
	config := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	txBuilder := config.TxConfig.NewTxBuilder()

	msg := getMsg(t, account)
	err := txBuilder.SetMsgs(msg)
	require.NoError(t, err)

	signature := signing.SignatureV2{
		PubKey: account.GetPubKey(), // Valid PubKey
		Data: &signing.SingleSignatureData{
			SignMode: signing.SignMode_SIGN_MODE_DIRECT,
		},
		Sequence: 0,
	}
	err = txBuilder.SetSignatures(signature)
	require.NoError(t, err)

	return txBuilder.GetTx()
}

func getMsg(t *testing.T, account sdk.AccountI) sdk.Msg {
	namespace, err := share.NewV0Namespace([]byte("CeroA"))
	require.NoError(t, err)

	blob, err := share.NewV0Blob(namespace, []byte("data"))
	require.NoError(t, err)

	signer := account.GetAddress().String()
	msg, err := types.NewMsgPayForBlobs(signer, appconsts.LatestVersion, blob)
	require.NoError(t, err)

	return msg
}
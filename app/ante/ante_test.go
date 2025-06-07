package ante_test

import (
	"fmt"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"

	"github.com/celestiaorg/go-square/v2/share"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/celestiaorg/celestia-app/v3/x/blob/types"
)

// Reproduces https://github.com/celestiaorg/celestia-app/issues/4847
func TestSigVerificationDecorator(t *testing.T) {
	testApp, _, _ := testutil.NewTestAppWithGenesisSet(app.DefaultConsensusParams())

	ctx := testApp.NewContext(false, tmproto.Header{})
	accounts := testApp.AccountKeeper.GetAllAccounts(ctx)
	require.NotEmpty(t, accounts)

	account, err := getAccountWithPubKey(accounts)
	require.NoError(t, err)

	fmt.Printf("account: %s\n", account.GetAddress().String())
	fmt.Printf("pubKey: %s\n", account.GetPubKey().String())

	encodingConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	signModeHandler := encodingConfig.TxConfig.SignModeHandler()
	decorator := ante.NewSigVerificationDecorator(testApp.AccountKeeper, signModeHandler)

	tx := createTxWithNilPubKey(t, account)
	simulate := false

	_, err = decorator.AnteHandle(ctx, tx, simulate, nextAnteHandler)
	require.Error(t, err) // "signature verification failed; please verify account number (0) and chain-id ()"
}

func getAccountWithPubKey(accounts []authtypes.AccountI) (authtypes.AccountI, error) {
	for _, account := range accounts {
		if account.GetPubKey() != nil {
			return account, nil
		}
	}
	return nil, fmt.Errorf("no account found with a pubkey")
}

func createTxWithNilPubKey(t *testing.T, account authtypes.AccountI) authsigning.Tx {
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

func getMsg(t *testing.T, account authtypes.AccountI) sdk.Msg {
	namespace, err := share.NewV0Namespace([]byte("CeroA"))
	require.NoError(t, err)

	blob, err := share.NewV0Blob(namespace, []byte("data"))
	require.NoError(t, err)

	signer := account.GetAddress().String()
	msg, err := types.NewMsgPayForBlobs(signer, appconsts.LatestVersion, blob)
	require.NoError(t, err)

	return msg
}

func nextAnteHandler(ctx sdk.Context, tx sdk.Tx, simulate bool) (sdk.Context, error) {
	return ctx, nil
}

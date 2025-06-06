package ante_test

import (
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

// TestIssue4847Integration reproduces the exact scenario from issue #4847
// where a MsgPayForBlobs transaction with nil PubKey in signature would cause a panic
func TestIssue4847Integration(t *testing.T) {
	testApp, _, _ := testutil.NewTestAppWithGenesisSet(app.DefaultConsensusParams())

	ctx := testApp.NewContext(false)
	accounts := testApp.AccountKeeper.GetAllAccounts(ctx)
	require.NotEmpty(t, accounts)

	account, err := getAccountWithPubKey(accounts)
	require.NoError(t, err)

	// Create a MsgPayForBlobs transaction with nil PubKey signature
	// This is the scenario that was causing the panic described in the issue
	tx := createMsgPayForBlobsWithNilPubKey(t, account)

	// Create the ante handler with the same configuration as the main app
	encodingConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	anteHandler := ante.NewAnteHandler(
		testApp.AccountKeeper,
		testApp.BankKeeper,
		testApp.BlobKeeper,
		testApp.FeeGrantKeeper,
		encodingConfig.TxConfig.SignModeHandler(),
		ante.DefaultSigVerificationGasConsumer,
		testApp.IBCKeeper,
		testApp.MinFeeKeeper,
		&testApp.CircuitKeeper,
		make(map[string]ante.ParamFilter), // Empty param filters for test
	)
	
	// Try to process the transaction through the ante handlers
	// Before the fix: this would panic with "invalid memory address or nil pointer dereference"
	// After the fix: this should return a descriptive error
	_, err = anteHandler(ctx, tx, false)
	require.Error(t, err)
	require.Contains(t, err.Error(), "nil PubKey")
	
	// Verify that the node continues to operate normally after this error
	// (no panic occurred)
	t.Logf("Successfully handled malformed transaction: %v", err)
}

func createMsgPayForBlobsWithNilPubKey(t *testing.T, account sdk.AccountI) authsigning.Tx {
	config := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	txBuilder := config.TxConfig.NewTxBuilder()

	// Create the exact MsgPayForBlobs that was mentioned in the issue
	// with namespace "CeroA" 
	namespace, err := share.NewV0Namespace([]byte("CeroA"))
	require.NoError(t, err)

	blob, err := share.NewV0Blob(namespace, make([]byte, 397730)) // Large blob as in the issue
	require.NoError(t, err)

	signer := account.GetAddress().String()
	msg, err := types.NewMsgPayForBlobs(signer, appconsts.LatestVersion, blob)
	require.NoError(t, err)

	err = txBuilder.SetMsgs(msg)
	require.NoError(t, err)

	// Create signature with nil PubKey - this is what causes the panic
	signature := signing.SignatureV2{
		PubKey: nil, // This nil PubKey is what causes the panic in the original issue
		Data: &signing.SingleSignatureData{
			SignMode: signing.SignMode_SIGN_MODE_DIRECT,
		},
		Sequence: 0,
	}
	err = txBuilder.SetSignatures(signature)
	require.NoError(t, err)

	return txBuilder.GetTx()
}
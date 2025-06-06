package ante_test

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/x/auth/ante"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	testutil "github.com/celestiaorg/celestia-app/v4/test/util"
)

// TestOriginalPanic demonstrates that the original cosmos-sdk SigVerificationDecorator
// would panic with nil PubKey, confirming our fix is necessary
func TestOriginalPanic(t *testing.T) {
	testApp, _, _ := testutil.NewTestAppWithGenesisSet(app.DefaultConsensusParams())

	ctx := testApp.NewContext(false)
	accounts := testApp.AccountKeeper.GetAllAccounts(ctx)
	require.NotEmpty(t, accounts)

	account, err := getAccountWithPubKey(accounts)
	require.NoError(t, err)

	// Create a transaction with nil PubKey signature
	tx := createMsgPayForBlobsWithNilPubKey(t, account)

	// Use the original cosmos-sdk decorator directly (not our safe wrapper)
	encodingConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	originalDecorator := ante.NewSigVerificationDecorator(testApp.AccountKeeper, encodingConfig.TxConfig.SignModeHandler())
	
	// This should panic with the original decorator
	require.Panics(t, func() {
		_, _ = originalDecorator.AnteHandle(ctx, tx, false, nextAnteHandler)
	}, "Expected original decorator to panic with nil PubKey")
	
	t.Logf("Confirmed: original decorator panics with nil PubKey")
}
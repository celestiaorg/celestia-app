package ante_test

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	appante "github.com/celestiaorg/celestia-app/v10/app/ante"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/sigcache"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// signedTx builds a bank send signed by account with the given sequence.
func signedTx(t *testing.T, txConfig client.TxConfig, kr keyring.Keyring, account string, accNum, sequence uint64) authsigning.Tx {
	t.Helper()

	signer, err := user.NewSigner(kr, txConfig, testutil.ChainID, user.NewAccount(account, accNum, sequence))
	require.NoError(t, err)

	addr := testfactory.GetAddress(kr, account)
	msg := banktypes.NewMsgSend(addr, addr, sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)))
	txBytes, _, err := signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(200_000), user.SetFee(2_000))
	require.NoError(t, err)

	decoded, err := txConfig.TxDecoder()(txBytes)
	require.NoError(t, err)
	return decoded.(authsigning.Tx)
}

// TestPreverifyTxSignaturesMatchesColdAnteDecision checks that warming the
// cache never changes what the ordered ante handler decides.
func TestPreverifyTxSignaturesMatchesColdAnteDecision(t *testing.T) {
	accounts := testfactory.GenerateAccounts(1)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	ctx := testApp.NewUncachedContext(false, cmtproto.Header{
		ChainID: testutil.ChainID,
		Height:  testApp.LastBlockHeight(),
		Time:    time.Now(),
	}).WithChainID(testutil.ChainID)

	acc := testApp.AccountKeeper.GetAccount(ctx, testfactory.GetAddress(kr, accounts[0]))
	require.NotNil(t, acc)
	// SetPubKeyDecorator records the key earlier in the chain; this test runs
	// the verification decorator alone, so it has to do the same.
	record, err := kr.Key(accounts[0])
	require.NoError(t, err)
	pubKey, err := record.GetPubKey()
	require.NoError(t, err)
	require.NoError(t, acc.SetPubKey(pubKey))
	testApp.AccountKeeper.SetAccount(ctx, acc)

	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	valid := signedTx(t, enc.TxConfig, kr, accounts[0], acc.GetAccountNumber(), acc.GetSequence())
	wrongSequence := signedTx(t, enc.TxConfig, kr, accounts[0], acc.GetAccountNumber(), acc.GetSequence()+5)
	wrongAccountNumber := signedTx(t, enc.TxConfig, kr, accounts[0], acc.GetAccountNumber()+1, acc.GetSequence())

	corrupted := signedTx(t, enc.TxConfig, kr, accounts[0], acc.GetAccountNumber(), acc.GetSequence())
	corruptedBytes, err := enc.TxConfig.TxEncoder()(corrupted)
	require.NoError(t, err)
	corruptedBytes[len(corruptedBytes)-1] ^= 0xff
	corruptedDecoded, err := enc.TxConfig.TxDecoder()(corruptedBytes)
	var tampered authsigning.Tx
	if err == nil {
		tampered = corruptedDecoded.(authsigning.Tx)
	}

	tests := map[string]struct {
		tx         authsigning.Tx
		wantAccept bool
		// wantWarm is whether the signature itself verifies, which is not the
		// same as the ante handler accepting the transaction: a signature over
		// the wrong sequence is valid for the bytes it signed, and the ante
		// handler rejects it on the sequence check instead.
		wantWarm bool
	}{
		"valid signature":      {valid, true, true},
		"wrong sequence":       {wrongSequence, false, true},
		"wrong account number": {wrongAccountNumber, false, false},
		"tampered signature":   {tampered, false, false},
	}

	for name, tc := range tests {
		require.NotNil(t, tc.tx, name)
		t.Run(name, func(t *testing.T) {
			cold := appante.NewCachedSigVerificationDecorator(testApp.AccountKeeper, enc.TxConfig.SignModeHandler(), sigcache.New(16))
			_, coldErr := cold.AnteHandle(ctx, tc.tx, false, nextAnteHandler)
			require.Equal(t, tc.wantAccept, coldErr == nil, "cold: %v", coldErr)

			warmCache := sigcache.New(16)
			warm := appante.NewCachedSigVerificationDecorator(testApp.AccountKeeper, enc.TxConfig.SignModeHandler(), warmCache)
			warm.PreverifyTxSignatures(ctx, []sdk.Tx{tc.tx})
			require.Equal(t, tc.wantWarm, warmCache.Len() > 0)

			_, warmErr := warm.AnteHandle(ctx, tc.tx, false, nextAnteHandler)
			require.Equal(t, coldErr == nil, warmErr == nil, "cold: %v, warm: %v", coldErr, warmErr)
			if coldErr != nil {
				require.Equal(t, coldErr.Error(), warmErr.Error())
			}
		})
	}

	// Warming a nil tx, a tx with no signatures and a tx for an account with no
	// recorded public key must not panic or poison the cache.
	t.Run("warming tolerates unusable transactions", func(t *testing.T) {
		cache := sigcache.New(16)
		d := appante.NewCachedSigVerificationDecorator(testApp.AccountKeeper, enc.TxConfig.SignModeHandler(), cache)
		require.NotPanics(t, func() {
			d.PreverifyTxSignatures(ctx, []sdk.Tx{nil, valid})
		})
		_, err := d.AnteHandle(ctx, valid, false, nextAnteHandler)
		require.NoError(t, err)
	})
}

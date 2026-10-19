package ante_test

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/ante"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	kmultisig "github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/crypto/types/multisig"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	xauthsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
)

const TxSizeCostPerByte = 8

func setup() (*app.App, sdk.Context, client.Context, error) {
	testApp, _, _ := testutil.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	ctx := testApp.NewContext(false)
	params := authtypes.DefaultParams()
	// Override default with a different TxSizeCostPerByte value for testing
	params.TxSizeCostPerByte = TxSizeCostPerByte
	if err := testApp.AccountKeeper.Params.Set(ctx, params); err != nil {
		return nil, sdk.Context{}, client.Context{}, err
	}
	ctx = ctx.WithBlockHeight(1)

	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	// We're using TestMsg encoding in the test, so register it here.
	testdata.RegisterInterfaces(enc.InterfaceRegistry)

	clientCtx := client.Context{}.
		WithTxConfig(enc.TxConfig)

	return testApp, ctx, clientCtx, nil
}

func TestConsumeGasForTxSize(t *testing.T) {
	testApp, ctx, clientCtx, err := setup()
	require.NoError(t, err)
	var txBuilder client.TxBuilder

	// keys and addresses
	priv1, _, addr1 := testdata.KeyTestPubAddr()

	// msg and signatures
	msg := testdata.NewTestMsg(addr1)
	feeAmount := testdata.NewTestFeeAmount()
	gasLimit := testdata.NewTestGasLimit()

	cgtsd := ante.NewConsumeGasForTxSizeDecorator(testApp.AccountKeeper)
	antehandler := sdk.ChainAnteDecorators(cgtsd)

	testCases := []struct {
		version uint64
		name    string
		sigV2   signing.SignatureV2
	}{
		{appconsts.Version, fmt.Sprintf("SingleSignatureData v%d", appconsts.Version), signing.SignatureV2{PubKey: priv1.PubKey()}},
		{appconsts.Version, fmt.Sprintf("MultiSignatureData v%d", appconsts.Version), signing.SignatureV2{PubKey: priv1.PubKey(), Data: multisig.NewMultisig(2)}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// set the version
			ctx = testApp.NewContext(false)
			err = testApp.SetAppVersion(ctx, tc.version)
			require.NoError(t, err)
			txBuilder = clientCtx.TxConfig.NewTxBuilder()
			require.NoError(t, txBuilder.SetMsgs(msg))
			txBuilder.SetFeeAmount(feeAmount)
			txBuilder.SetGasLimit(gasLimit)
			txBuilder.SetMemo(strings.Repeat("01234567890", 10))

			privs, accNums, accSeqs := []cryptotypes.PrivKey{priv1}, []uint64{0}, []uint64{0}
			tx, err := createTestTx(txBuilder, clientCtx, privs, accNums, accSeqs, ctx.ChainID())
			require.NoError(t, err)

			txBytes, err := clientCtx.TxConfig.TxJSONEncoder()(tx)
			require.Nil(t, err, "Cannot marshal tx: %v", err)

			txSizeCostPerByte := appconsts.TxSizeCostPerByte
			expectedGas := storetypes.Gas(len(txBytes)) * txSizeCostPerByte

			ctx = ctx.WithTxBytes(txBytes)

			// track how much gas is necessary to retrieve parameters
			beforeGas := ctx.GasMeter().GasConsumed()
			afterGas := ctx.GasMeter().GasConsumed()
			expectedGas += afterGas - beforeGas

			beforeGas = ctx.GasMeter().GasConsumed()
			ctx, err = antehandler(ctx, tx, false)
			require.NoError(t, err)
			require.Nil(t, err, "ConsumeTxSizeGasDecorator returned error: %v", err)

			// require that decorator consumes expected amount of gas
			consumedGas := ctx.GasMeter().GasConsumed() - beforeGas
			require.Equal(t, expectedGas, consumedGas, "Decorator did not consume the correct amount of gas")

			// simulation must not underestimate gas of this decorator even with nil signatures
			txBuilder, err := clientCtx.TxConfig.WrapTxBuilder(tx)
			require.NoError(t, err)
			require.NoError(t, txBuilder.SetSignatures(tc.sigV2))
			tx = txBuilder.GetTx()

			simTxBytes, err := clientCtx.TxConfig.TxJSONEncoder()(tx)
			require.Nil(t, err, "Cannot marshal tx: %v", err)
			// require that simulated tx is smaller than tx with signatures
			require.True(t, len(simTxBytes) < len(txBytes), "simulated tx still has signatures")

			// Set ctx with smaller simulated TxBytes manually
			ctx = ctx.WithTxBytes(simTxBytes).WithExecMode(sdk.ExecModeSimulate)

			beforeSimGas := ctx.GasMeter().GasConsumed()

			// run antehandler with simulate=true
			ctx, err = antehandler(ctx, tx, true)
			require.NoError(t, err)
			consumedSimGas := ctx.GasMeter().GasConsumed() - beforeSimGas

			// require that antehandler passes and does not underestimate decorator cost
			require.Nil(t, err, "ConsumeTxSizeGasDecorator returned error: %v", err)
			require.True(t, consumedSimGas >= expectedGas, "Simulate mode underestimates gas on AnteDecorator. Simulated cost: %d, expected cost: %d", consumedSimGas, expectedGas)
		})
	}
}

// createTestTx creates a test tx given multiple inputs.
func createTestTx(txBuilder client.TxBuilder, clientCtx client.Context, privs []cryptotypes.PrivKey, accNums, accSeqs []uint64, chainID string) (xauthsigning.Tx, error) {
	// First round: we gather all the signer infos. We use the "set empty
	// signature" hack to do that.
	sigsV2 := make([]signing.SignatureV2, 0, len(privs))

	for i, priv := range privs {
		sigV2 := signing.SignatureV2{
			PubKey: priv.PubKey(),
			Data: &signing.SingleSignatureData{
				SignMode:  signing.SignMode(clientCtx.TxConfig.SignModeHandler().DefaultMode()),
				Signature: nil,
			},
			Sequence: accSeqs[i],
		}

		sigsV2 = append(sigsV2, sigV2)
	}

	if err := txBuilder.SetSignatures(sigsV2...); err != nil {
		return nil, err
	}

	// Second round: all signer infos are set, so each signer can sign.
	sigsV2 = []signing.SignatureV2{}
	for i, priv := range privs {
		signerData := xauthsigning.SignerData{
			ChainID:       chainID,
			AccountNumber: accNums[i],
			Sequence:      accSeqs[i],
		}
		sigV2, err := tx.SignWithPrivKey(clientCtx.CmdContext,
			signing.SignMode(clientCtx.TxConfig.SignModeHandler().DefaultMode()), signerData,
			txBuilder, priv, clientCtx.TxConfig, accSeqs[i])
		if err != nil {
			return nil, err
		}

		sigsV2 = append(sigsV2, sigV2)
	}

	if err := txBuilder.SetSignatures(sigsV2...); err != nil {
		return nil, err
	}

	return txBuilder.GetTx(), nil
}

// sigTx is a SigVerifiableTx with fixed signatures or a signature read error.
type sigTx struct {
	xauthsigning.SigVerifiableTx
	sigs []signing.SignatureV2
	err  error
}

func (tx sigTx) GetSignaturesV2() ([]signing.SignatureV2, error) { return tx.sigs, tx.err }

// unsupportedTx does not implement SigVerifiableTx.
type unsupportedTx struct{ sdk.Tx }

// runTxSizeGas runs the decorator on 100 tx bytes with the given gas limit. It
// returns the gas consumed and whether next was called.
func runTxSizeGas(t *testing.T, testApp *app.App, gasLimit uint64, tx sdk.Tx, simulate bool) (uint64, bool, error) {
	t.Helper()
	ctx := testApp.NewContext(false).
		WithTxBytes(make([]byte, 100)).
		WithGasMeter(storetypes.NewGasMeter(gasLimit))
	nextCalled := false
	next := func(ctx sdk.Context, _ sdk.Tx, sim bool) (sdk.Context, error) {
		require.Equal(t, simulate, sim)
		nextCalled = true
		return ctx, nil
	}
	_, err := ante.NewConsumeGasForTxSizeDecorator(testApp.AccountKeeper).AnteHandle(ctx, tx, simulate, next)
	return ctx.GasMeter().GasConsumed(), nextCalled, err
}

func TestConsumeGasForTxSizeEdgeCases(t *testing.T) {
	testApp, _, _, err := setup()
	require.NoError(t, err)

	const txSizeGas = 100 * appconsts.TxSizeCostPerByte
	// Each simulated signature reads the auth params, which costs store gas.
	paramsCtx := testApp.NewContext(false).WithGasMeter(storetypes.NewInfiniteGasMeter())
	sigLimit := testApp.AccountKeeper.GetParams(paramsCtx).TxSigLimit
	paramsGas := paramsCtx.GasMeter().GasConsumed()
	require.NotZero(t, paramsGas)
	// An amino StdSignature with a secp256k1 pubkey is 106 bytes, plus 6.
	singleSigGas := paramsGas + appconsts.TxSizeCostPerByte*(106+6)
	// An amino StdSignature with a 2-of-2 multisig pubkey is 154 bytes, plus 6,
	// charged TxSigLimit times.
	multiSigGas := paramsGas + appconsts.TxSizeCostPerByte*(154+6)*sigLimit

	priv1, _, _ := testdata.KeyTestPubAddr()
	priv2, _, _ := testdata.KeyTestPubAddr()
	pk1, pk2 := priv1.PubKey(), priv2.PubKey()
	multisigPk := kmultisig.NewLegacyAminoPubKey(2, []cryptotypes.PubKey{pk1, pk2})
	complete := &signing.SingleSignatureData{Signature: []byte{1}}
	empty := &signing.SingleSignatureData{}

	testCases := []struct {
		name   string
		sigs   []signing.SignatureV2
		simGas uint64
	}{
		{"no signatures", nil, txSizeGas},
		{"nil data and nil pubkey", []signing.SignatureV2{{}}, txSizeGas + singleSigGas},
		{"empty single signature", []signing.SignatureV2{{PubKey: pk1, Data: empty}}, txSizeGas + singleSigGas},
		{"complete single signature", []signing.SignatureV2{{PubKey: pk1, Data: complete}}, txSizeGas},
		{"multisig without signatures", []signing.SignatureV2{{PubKey: pk1, Data: &signing.MultiSignatureData{}}}, txSizeGas + singleSigGas},
		{"incomplete multisig", []signing.SignatureV2{{PubKey: pk1, Data: &signing.MultiSignatureData{Signatures: []signing.SignatureData{complete, empty}}}}, txSizeGas + singleSigGas},
		{"incomplete nested multisig", []signing.SignatureV2{{PubKey: pk1, Data: &signing.MultiSignatureData{Signatures: []signing.SignatureData{complete, &signing.MultiSignatureData{Signatures: []signing.SignatureData{empty}}}}}}, txSizeGas + singleSigGas},
		{"complete nested multisig", []signing.SignatureV2{{PubKey: pk1, Data: &signing.MultiSignatureData{Signatures: []signing.SignatureData{complete, &signing.MultiSignatureData{Signatures: []signing.SignatureData{complete}}}}}}, txSizeGas},
		{"multisig pubkey", []signing.SignatureV2{{PubKey: multisigPk, Data: empty}}, txSizeGas + multiSigGas},
		{"one of two signers incomplete", []signing.SignatureV2{{PubKey: pk1, Data: complete}, {PubKey: pk2}}, txSizeGas + singleSigGas},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			tx := sigTx{sigs: tc.sigs}
			// Repeated runs must charge the same gas.
			for range 2 {
				gas, nextCalled, err := runTxSizeGas(t, testApp, tc.simGas, tx, true)
				require.NoError(t, err)
				require.True(t, nextCalled)
				require.Equal(t, tc.simGas, gas)

				// Outside simulation only the tx bytes are charged.
				gas, nextCalled, err = runTxSizeGas(t, testApp, txSizeGas, tx, false)
				require.NoError(t, err)
				require.True(t, nextCalled)
				require.Equal(t, txSizeGas, gas)
			}

			require.PanicsWithValue(t, storetypes.ErrorOutOfGas{Descriptor: "txSize"}, func() {
				_, _, _ = runTxSizeGas(t, testApp, tc.simGas-1, tx, true)
			})
		})
	}

	t.Run("unsupported tx type", func(t *testing.T) {
		gas, nextCalled, err := runTxSizeGas(t, testApp, txSizeGas, unsupportedTx{}, false)
		require.ErrorIs(t, err, sdkerrors.ErrTxDecode)
		require.False(t, nextCalled)
		require.Zero(t, gas)
	})

	t.Run("signature read error", func(t *testing.T) {
		readErr := errors.New("bad signatures")
		tx := sigTx{err: readErr}

		gas, nextCalled, err := runTxSizeGas(t, testApp, txSizeGas, tx, true)
		require.ErrorIs(t, err, readErr)
		require.False(t, nextCalled)
		require.Equal(t, txSizeGas, gas)

		// Signatures are only read in simulate mode.
		_, nextCalled, err = runTxSizeGas(t, testApp, txSizeGas, tx, false)
		require.NoError(t, err)
		require.True(t, nextCalled)
	})
}

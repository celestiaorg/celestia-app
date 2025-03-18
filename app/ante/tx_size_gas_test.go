package ante_test

import (
	"fmt"
	"strings"
	"testing"

	storetypes "cosmossdk.io/store/types"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/crypto/types/multisig"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	xauthsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/ante"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v4/test/util"
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

	enc := encoding.MakeTestConfig(app.ModuleEncodingRegisters...)
	// We're using TestMsg encoding in the test, so register it here.
	enc.Amino.RegisterConcrete(&testdata.TestMsg{}, "testdata.TestMsg", nil)
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
		{appconsts.LatestVersion, fmt.Sprintf("SingleSignatureData v%d", appconsts.LatestVersion), signing.SignatureV2{PubKey: priv1.PubKey()}},
		{appconsts.LatestVersion, fmt.Sprintf("MultiSignatureData v%d", appconsts.LatestVersion), signing.SignatureV2{PubKey: priv1.PubKey(), Data: multisig.NewMultisig(2)}},
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

			txSizeCostPerByte := appconsts.DefaultTxSizeCostPerByte
			expectedGas := storetypes.Gas(len(txBytes)) * txSizeCostPerByte

			// set suite.ctx with TxBytes manually
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

			// Set suite.ctx with smaller simulated TxBytes manually
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

package ante_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/ante"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	v2 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v2"
	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cometbft/cometbft/proto/tendermint/version"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/crypto/types/multisig"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	moduletestutil "github.com/cosmos/cosmos-sdk/types/module/testutil"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	xauthsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
)

const TxSizeCostPerByte = 8

func setup() (*app.App, sdk.Context, client.Context, error) {
	app, _, _ := testutil.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	ctx := app.NewContext(false, tmproto.Header{})
	params := authtypes.DefaultParams()
	// Override default with a different TxSizeCostPerByte value for testing
	params.TxSizeCostPerByte = TxSizeCostPerByte
	app.AccountKeeper.SetParams(ctx, params)
	ctx = ctx.WithBlockHeight(1)

	// Set up TxConfig.
	encodingConfig := moduletestutil.MakeTestEncodingConfig()
	// We're using TestMsg encoding in the test, so register it here.
	encodingConfig.Amino.RegisterConcrete(&testdata.TestMsg{}, "testdata.TestMsg", nil)
	testdata.RegisterInterfaces(encodingConfig.InterfaceRegistry)

	clientCtx := client.Context{}.
		WithTxConfig(encodingConfig.TxConfig)

	return app, ctx, clientCtx, nil
}

func TestConsumeGasForTxSize(t *testing.T) {
	app, ctx, clientCtx, err := setup()
	require.NoError(t, err)
	var txBuilder client.TxBuilder

	// keys and addresses
	priv1, _, addr1 := testdata.KeyTestPubAddr()

	// msg and signatures
	msg := testdata.NewTestMsg(addr1)
	feeAmount := testdata.NewTestFeeAmount()
	gasLimit := testdata.NewTestGasLimit()

	cgtsd := ante.NewConsumeGasForTxSizeDecorator(app.AccountKeeper)
	antehandler := sdk.ChainAnteDecorators(cgtsd)

	testCases := []struct {
		version uint64
		name    string
		sigV2   signing.SignatureV2
	}{
		{v2.Version, "SingleSignatureData v2", signing.SignatureV2{PubKey: priv1.PubKey()}},
		{v2.Version, "MultiSignatureData v2", signing.SignatureV2{PubKey: priv1.PubKey(), Data: multisig.NewMultisig(2)}},
		{appconsts.LatestVersion, fmt.Sprintf("SingleSignatureData v%d", appconsts.LatestVersion), signing.SignatureV2{PubKey: priv1.PubKey()}},
		{appconsts.LatestVersion, fmt.Sprintf("MultiSignatureData v%d", appconsts.LatestVersion), signing.SignatureV2{PubKey: priv1.PubKey(), Data: multisig.NewMultisig(2)}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			// set the version
			ctx = app.NewContext(false, tmproto.Header{Version: version.Consensus{
				App: tc.version,
			}})

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

			// expected TxSizeCostPerByte is different for each version
			var txSizeCostPerByte uint64
			if tc.version == v2.Version {
				txSizeCostPerByte = TxSizeCostPerByte
			} else {
				txSizeCostPerByte = appconsts.TxSizeCostPerByte(tc.version)
			}

			expectedGas := sdk.Gas(len(txBytes)) * txSizeCostPerByte

			// set suite.ctx with TxBytes manually
			ctx = ctx.WithTxBytes(txBytes)

			// track how much gas is necessary to retrieve parameters
			beforeGas := ctx.GasMeter().GasConsumed()
			app.AccountKeeper.GetParams(ctx)
			afterGas := ctx.GasMeter().GasConsumed()
			expectedGas += afterGas - beforeGas

			beforeGas = ctx.GasMeter().GasConsumed()
			ctx, err = antehandler(ctx, tx, false)
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
			ctx = ctx.WithTxBytes(simTxBytes)

			beforeSimGas := ctx.GasMeter().GasConsumed()

			// run antehandler with simulate=true
			ctx, err = antehandler(ctx, tx, true)
			consumedSimGas := ctx.GasMeter().GasConsumed() - beforeSimGas

			// require that antehandler passes and does not underestimate decorator cost
			require.Nil(t, err, "ConsumeTxSizeGasDecorator returned error: %v", err)
			require.True(t, consumedSimGas >= expectedGas, "Simulate mode underestimates gas on AnteDecorator. Simulated cost: %d, expected cost: %d", consumedSimGas, expectedGas)
		})
	}
}

// createTestTx creates a test tx given multiple inputs.
func createTestTx(txBuilder client.TxBuilder, clientCtx client.Context, privs []cryptotypes.PrivKey, accNums []uint64, accSeqs []uint64, chainID string) (xauthsigning.Tx, error) {
	// First round: we gather all the signer infos. We use the "set empty
	// signature" hack to do that.
	sigsV2 := make([]signing.SignatureV2, 0, len(privs))
	for i, priv := range privs {
		sigV2 := signing.SignatureV2{
			PubKey: priv.PubKey(),
			Data: &signing.SingleSignatureData{
				SignMode:  clientCtx.TxConfig.SignModeHandler().DefaultMode(),
				Signature: nil,
			},
			Sequence: accSeqs[i],
		}

		sigsV2 = append(sigsV2, sigV2)
	}
	err := txBuilder.SetSignatures(sigsV2...)
	if err != nil {
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
		sigV2, err := tx.SignWithPrivKey(
			clientCtx.TxConfig.SignModeHandler().DefaultMode(), signerData,
			txBuilder, priv, clientCtx.TxConfig, accSeqs[i])
		if err != nil {
			return nil, err
		}

		sigsV2 = append(sigsV2, sigV2)
	}
	err = txBuilder.SetSignatures(sigsV2...)
	if err != nil {
		return nil, err
	}

	return txBuilder.GetTx(), nil
}

package ante_test

import (
	"fmt"
	"strings"
	"testing"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/ante"
	"github.com/celestiaorg/celestia-app/v3/pkg/appconsts"
	v3 "github.com/celestiaorg/celestia-app/v3/pkg/appconsts/v3"
	testutil "github.com/celestiaorg/celestia-app/v3/test/util"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/tx"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/crypto/types/multisig"
	"github.com/cosmos/cosmos-sdk/simapp"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authante "github.com/cosmos/cosmos-sdk/x/auth/ante"
	xauthsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	require "github.com/stretchr/testify/require"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

func setup() (*app.App, sdk.Context, client.Context, sdk.AnteHandler, error) {
	app, _, _ := testutil.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	ctx := app.NewContext(false, tmproto.Header{})
	app.AccountKeeper.SetParams(ctx, authtypes.DefaultParams())
	ctx = ctx.WithBlockHeight(1)

	// Set up TxConfig.
	encodingConfig := simapp.MakeTestEncodingConfig()
	// We're using TestMsg encoding in some tests, so register it here.
	encodingConfig.Amino.RegisterConcrete(&testdata.TestMsg{}, "testdata.TestMsg", nil)
	testdata.RegisterInterfaces(encodingConfig.InterfaceRegistry)

	clientCtx := client.Context{}.
		WithTxConfig(encodingConfig.TxConfig)

	anteHandler, err := authante.NewAnteHandler(
		authante.HandlerOptions{
			AccountKeeper:   app.AccountKeeper,
			BankKeeper:      app.BankKeeper,
			FeegrantKeeper:  app.FeeGrantKeeper,
			SignModeHandler: encodingConfig.TxConfig.SignModeHandler(),
			SigGasConsumer:  authante.DefaultSigVerificationGasConsumer,
		},
	)
	if err != nil {
		return nil, sdk.Context{}, client.Context{}, nil, fmt.Errorf("error creating AnteHandler: %v", err)
	}

	return app, ctx, clientCtx, anteHandler, nil
}

func TestConsumeGasForTxSize(t *testing.T) {
	app, ctx, clientCtx, _, err := setup()
	require.NoError(t, err)
	sub, exs := app.ParamsKeeper.GetSubspace(authtypes.ModuleName)
	fmt.Println(sub, exs)
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
		name  string
		sigV2 signing.SignatureV2
	}{
		{"SingleSignatureData", signing.SignatureV2{PubKey: priv1.PubKey()}},
		{"MultiSignatureData", signing.SignatureV2{PubKey: priv1.PubKey(), Data: multisig.NewMultisig(2)}},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			txBuilder = clientCtx.TxConfig.NewTxBuilder()
			require.NoError(t, txBuilder.SetMsgs(msg))
			txBuilder.SetFeeAmount(feeAmount)
			txBuilder.SetGasLimit(gasLimit)
			txBuilder.SetMemo(strings.Repeat("01234567890", 10))

			privs, accNums, accSeqs := []cryptotypes.PrivKey{priv1}, []uint64{0}, []uint64{0}
			tx, err := CreateTestTx(txBuilder, clientCtx, privs, accNums, accSeqs, ctx.ChainID())
			require.NoError(t, err)

			txBytes, err := clientCtx.TxConfig.TxJSONEncoder()(tx)
			require.Nil(t, err, "Cannot marshal tx: %v", err)

			expectedGas := sdk.Gas(len(txBytes)) * appconsts.TxSizeCostPerByte(v3.Version)

			// Set suite.ctx with TxBytes manually
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

// CreateTestTx is a helper function to create a tx given multiple inputs.
func CreateTestTx(txBuilder client.TxBuilder, clientCtx client.Context, privs []cryptotypes.PrivKey, accNums []uint64, accSeqs []uint64, chainID string) (xauthsigning.Tx, error) {
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

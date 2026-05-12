package ante_test

import (
	"context"
	"encoding/hex"
	"testing"

	txsigning "cosmossdk.io/x/tx/signing"
	"github.com/celestiaorg/celestia-app/v9/app"
	appante "github.com/celestiaorg/celestia-app/v9/app/ante"
	"github.com/celestiaorg/celestia-app/v9/app/encoding"
	"github.com/celestiaorg/celestia-app/v9/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v9/pkg/tx/eip712"
	testutil "github.com/celestiaorg/celestia-app/v9/test/util"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
)

func TestEIP712SetPubKeyDecoratorRecoversMissingPubKey(t *testing.T) {
	testApp, ctx, tx := buildEIP712Tx(t, false)
	decorator := appante.NewEIP712SetPubKeyDecorator(testApp.AccountKeeper)

	beforeGas := ctx.GasMeter().GasConsumed()
	ctx, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)
	require.NoError(t, err)

	signers, err := tx.GetSigners()
	require.NoError(t, err)
	acc := testApp.AccountKeeper.GetAccount(ctx, signers[0])
	require.NotNil(t, acc.GetPubKey())
	require.GreaterOrEqual(t, ctx.GasMeter().GasConsumed(), beforeGas+appante.EIP712PubKeyRecoveryGas)
}

func TestEIP712SetPubKeyDecoratorRejectsWrongSigner(t *testing.T) {
	testApp, ctx, tx := buildEIP712Tx(t, true)
	decorator := appante.NewEIP712SetPubKeyDecorator(testApp.AccountKeeper)

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)
	require.ErrorContains(t, err, "does not match canonical signer")
}

func TestEIP712ValidateSigCountDecoratorCountsRecoveredSignature(t *testing.T) {
	testApp, ctx, tx := buildEIP712Tx(t, false)
	decorator := appante.NewEIP712ValidateSigCountDecorator(testApp.AccountKeeper)

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)
	require.NoError(t, err)
}

func TestEIP712FullAnteHandlerAcceptsRecoveredSingleSigner(t *testing.T) {
	fee := sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1000))
	testApp, ctx, tx := buildEIP712Tx(t, false, withEIP712Fee(fee), withEIP712GasLimit(200000))
	signers, err := tx.GetSigners()
	require.NoError(t, err)
	require.Len(t, signers, 1)
	fundAccount(t, testApp, ctx, signers[0], sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1000000)))

	txBytes, err := testApp.GetTxConfig().TxEncoder()(tx)
	require.NoError(t, err)
	ctx = ctx.WithTxBytes(txBytes)

	handler := appante.NewAnteHandler(
		testApp.AccountKeeper,
		testApp.BankKeeper,
		testApp.BlobKeeper,
		testApp.FeeGrantKeeper,
		testApp.GetTxConfig().SignModeHandler(),
		appante.DefaultSigVerificationGasConsumer,
		testApp.IBCKeeper,
		testApp.MinFeeKeeper,
		&testApp.CircuitKeeper,
		testApp.GovParamFilters(),
	)
	ctx, err = handler(ctx, tx, false)
	require.NoError(t, err)

	acc := testApp.AccountKeeper.GetAccount(ctx, signers[0])
	require.NotNil(t, acc.GetPubKey())
	require.Equal(t, uint64(1), acc.GetSequence())
}

func TestEIP712FullAnteHandlerRejectsTamperedSignature(t *testing.T) {
	fee := sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1000))
	testApp, ctx, tx := buildEIP712Tx(t, false, withEIP712Fee(fee), withEIP712GasLimit(200000), withTamperedEIP712Signature())
	signers, err := tx.GetSigners()
	require.NoError(t, err)
	fundAccount(t, testApp, ctx, signers[0], sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1000000)))

	txBytes, err := testApp.GetTxConfig().TxEncoder()(tx)
	require.NoError(t, err)
	ctx = ctx.WithTxBytes(txBytes)

	handler := appante.NewAnteHandler(
		testApp.AccountKeeper,
		testApp.BankKeeper,
		testApp.BlobKeeper,
		testApp.FeeGrantKeeper,
		testApp.GetTxConfig().SignModeHandler(),
		appante.DefaultSigVerificationGasConsumer,
		testApp.IBCKeeper,
		testApp.MinFeeKeeper,
		&testApp.CircuitKeeper,
		testApp.GovParamFilters(),
	)
	_, err = handler(ctx, tx, false)
	require.ErrorContains(t, err, "EIP-712")
}

func TestEIP712SetPubKeyDecoratorRejectsMultipleSigners(t *testing.T) {
	testApp, ctx, tx := buildEIP712Tx(t, false, withAdditionalEIP712Signer())
	decorator := appante.NewEIP712SetPubKeyDecorator(testApp.AccountKeeper)

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)
	require.ErrorContains(t, err, "EIP-712 supports exactly one signer")
}

func TestEIP712ValidateSigCountDecoratorRejectsMultipleSignatures(t *testing.T) {
	testApp, ctx, tx := buildEIP712Tx(t, false, withAdditionalEIP712Signature())
	decorator := appante.NewEIP712ValidateSigCountDecorator(testApp.AccountKeeper)

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)
	require.ErrorContains(t, err, "EIP-712 supports exactly one single signature")
}

type eip712TxOptions struct {
	fee                 sdk.Coins
	gasLimit            uint64
	additionalSigner    bool
	additionalSignature bool
	tamperSignature     bool
}

type eip712TxOption func(*eip712TxOptions)

func withEIP712Fee(fee sdk.Coins) eip712TxOption {
	return func(opts *eip712TxOptions) {
		opts.fee = fee
	}
}

func withEIP712GasLimit(gasLimit uint64) eip712TxOption {
	return func(opts *eip712TxOptions) {
		opts.gasLimit = gasLimit
	}
}

func withAdditionalEIP712Signer() eip712TxOption {
	return func(opts *eip712TxOptions) {
		opts.additionalSigner = true
	}
}

func withAdditionalEIP712Signature() eip712TxOption {
	return func(opts *eip712TxOptions) {
		opts.additionalSignature = true
	}
}

func withTamperedEIP712Signature() eip712TxOption {
	return func(opts *eip712TxOptions) {
		opts.tamperSignature = true
	}
}

func buildEIP712Tx(t *testing.T, wrongSigner bool, optionFns ...eip712TxOption) (*app.App, sdk.Context, authsigning.Tx) {
	t.Helper()
	opts := eip712TxOptions{gasLimit: 100000}
	for _, apply := range optionFns {
		apply(&opts)
	}

	testApp, _, _ := testutil.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	ctx := testApp.NewContext(false).WithBlockHeight(1)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	testdata.RegisterInterfaces(enc.InterfaceRegistry)
	clientCtx := client.Context{}.WithTxConfig(enc.TxConfig).WithCmdContext(context.Background())

	privBytes, err := hex.DecodeString("4c0883a69102937d6231471b5dbb6204fe512961708279b727a63ca9b9a4b4f3")
	require.NoError(t, err)
	priv, err := gethcrypto.ToECDSA(privBytes)
	require.NoError(t, err)
	pubKey := &secp256k1.PubKey{Key: gethcrypto.CompressPubkey(&priv.PublicKey)}
	signer := sdk.AccAddress(pubKey.Address())
	if wrongSigner {
		signer = sdk.AccAddress(make([]byte, len(signer)))
	}
	acc := testApp.AccountKeeper.NewAccountWithAddress(ctx, signer)
	testApp.AccountKeeper.SetAccount(ctx, acc)

	txBuilder := clientCtx.TxConfig.NewTxBuilder()
	msgSigners := []sdk.AccAddress{signer}
	if opts.additionalSigner {
		msgSigners = append(msgSigners, sdk.AccAddress(make([]byte, len(signer))))
	}
	require.NoError(t, txBuilder.SetMsgs(testdata.NewTestMsg(msgSigners...)))
	txBuilder.SetGasLimit(opts.gasLimit)
	if opts.fee != nil {
		txBuilder.SetFeeAmount(opts.fee)
	}
	ext, err := eip712.NewExtensionOptions(eip712.SchemaVersion, 12345)
	require.NoError(t, err)
	extBuilder, ok := txBuilder.(authtx.ExtensionOptionsTxBuilder)
	require.True(t, ok)
	extBuilder.SetExtensionOptions(ext)

	emptySig := signingtypes.SignatureV2{
		Data:     &signingtypes.SingleSignatureData{SignMode: signingtypes.SignMode_SIGN_MODE_EIP_712},
		Sequence: 0,
	}
	require.NoError(t, txBuilder.SetSignatures(emptySig))

	txData := txBuilder.GetTx().(authsigning.V2AdaptableTx).GetSigningTxData()
	signerData := txsigning.SignerData{
		Address:       signer.String(),
		ChainID:       ctx.ChainID(),
		AccountNumber: acc.GetAccountNumber(),
		Sequence:      acc.GetSequence(),
	}
	digest, err := eip712.Digest(signerData, txData)
	require.NoError(t, err)
	ethSig, err := gethcrypto.Sign(digest[:], priv)
	require.NoError(t, err)
	if opts.tamperSignature {
		ethSig[len(ethSig)-1] ^= 1
	}

	sig := signingtypes.SignatureV2{
		Data: &signingtypes.SingleSignatureData{
			SignMode:  signingtypes.SignMode_SIGN_MODE_EIP_712,
			Signature: ethSig,
		},
		Sequence: 0,
	}
	sigs := []signingtypes.SignatureV2{sig}
	if opts.additionalSignature {
		sigs = append(sigs, signingtypes.SignatureV2{
			Data: &signingtypes.SingleSignatureData{
				SignMode:  signingtypes.SignMode_SIGN_MODE_DIRECT,
				Signature: []byte("unsupported-second-signature"),
			},
			Sequence: 0,
		})
	}
	require.NoError(t, txBuilder.SetSignatures(sigs...))
	return testApp, ctx, txBuilder.GetTx()
}

func fundAccount(t *testing.T, testApp *app.App, ctx sdk.Context, addr sdk.AccAddress, coins sdk.Coins) {
	t.Helper()
	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, coins))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, addr, coins))
}

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
	txethereum "github.com/celestiaorg/celestia-app/v9/pkg/tx/ethereum"
	testutil "github.com/celestiaorg/celestia-app/v9/test/util"
	ethidentitykeeper "github.com/celestiaorg/celestia-app/v9/x/ethidentity/keeper"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/testutil/testdata"
	sdk "github.com/cosmos/cosmos-sdk/types"
	signingtypes "github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtx "github.com/cosmos/cosmos-sdk/x/auth/tx"
	minttypes "github.com/cosmos/cosmos-sdk/x/mint/types"
	gethcrypto "github.com/ethereum/go-ethereum/crypto"
	"github.com/stretchr/testify/require"
	protov2 "google.golang.org/protobuf/proto"
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

func TestEthIdentityIndexDecoratorOnlyIndexesDuringFinalize(t *testing.T) {
	testApp, ctx, tx := buildEIP712Tx(t, false)
	recoveryDecorator := appante.NewEIP712SetPubKeyDecorator(testApp.AccountKeeper)
	ctx, err := recoveryDecorator.AnteHandle(ctx, tx, false, nextAnteHandler)
	require.NoError(t, err)

	identityKeeper := &recordingEthIdentityKeeper{}
	decorator := appante.NewEthIdentityIndexDecorator(testApp.AccountKeeper, identityKeeper)
	_, err = decorator.AnteHandle(ctx, tx, false, nextAnteHandler)
	require.NoError(t, err)
	require.Zero(t, identityKeeper.calls)

	_, err = decorator.AnteHandle(ctx.WithExecMode(sdk.ExecModeFinalize), tx, false, nextAnteHandler)
	require.NoError(t, err)
	require.Equal(t, 1, identityKeeper.calls)
}

func TestEthIdentityIndexDecoratorIndexesExistingSecpPubKey(t *testing.T) {
	testApp, _, _ := testutil.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	ctx := testApp.NewContext(false).WithExecMode(sdk.ExecModeFinalize)

	pubKey := secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey)
	signer := sdk.AccAddress(pubKey.Address())
	acc := testApp.AccountKeeper.NewAccountWithAddress(ctx, signer)
	require.NoError(t, acc.SetPubKey(pubKey))
	testApp.AccountKeeper.SetAccount(ctx, acc)

	tx := signerOnlyTx{signers: [][]byte{signer}}
	decorator := appante.NewEthIdentityIndexDecorator(testApp.AccountKeeper, testApp.EthIdentityKeeper)
	ctx, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)
	require.NoError(t, err)

	ethAddr, err := ethidentitykeeper.EthereumAddressFromPubKey(pubKey)
	require.NoError(t, err)
	resolved, found := testApp.EthIdentityKeeper.Resolve(ctx, ethAddr)
	require.True(t, found)
	require.Equal(t, signer, resolved)
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
	ctx = ctx.WithTxBytes(txBytes).WithExecMode(sdk.ExecModeFinalize)

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
		testApp.EthIdentityKeeper,
		testApp.GovParamFilters(),
	)
	ctx, err = handler(ctx, tx, false)
	require.NoError(t, err)

	acc := testApp.AccountKeeper.GetAccount(ctx, signers[0])
	require.NotNil(t, acc.GetPubKey())
	require.Equal(t, uint64(1), acc.GetSequence())

	pubKey, ok := acc.GetPubKey().(*secp256k1.PubKey)
	require.True(t, ok)
	ethAddr, err := ethidentitykeeper.EthereumAddressFromPubKey(pubKey)
	require.NoError(t, err)
	resolved, found := testApp.EthIdentityKeeper.Resolve(ctx, ethAddr)
	require.True(t, found)
	require.Equal(t, sdk.AccAddress(signers[0]), resolved)
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
		testApp.EthIdentityKeeper,
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

func TestEthereumTxAuthorizationRequiresExtensionOption(t *testing.T) {
	testApp, ctx, tx := buildEthereumTxPairingTx(t, false, true)
	decorator := appante.NewEthereumTxAuthorizationDecorator(testApp.AccountKeeper, testApp.EthIdentityKeeper)

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)
	require.ErrorContains(t, err, "SIGN_MODE_ETHEREUM_TX requires ExtensionOptionsEthereumTx")
}

func TestEthereumTxAuthorizationRequiresSignMode(t *testing.T) {
	testApp, ctx, tx := buildEthereumTxPairingTx(t, true, false)
	decorator := appante.NewEthereumTxAuthorizationDecorator(testApp.AccountKeeper, testApp.EthIdentityKeeper)

	_, err := decorator.AnteHandle(ctx, tx, false, nextAnteHandler)
	require.ErrorContains(t, err, "SIGN_MODE_ETHEREUM_TX requires ExtensionOptionsEthereumTx")
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

func buildEthereumTxPairingTx(t *testing.T, includeExt bool, includeEthereumSignMode bool) (*app.App, sdk.Context, authsigning.Tx) {
	t.Helper()

	testApp, _, _ := testutil.NewTestAppWithGenesisSet(app.DefaultConsensusParams())
	ctx := testApp.NewContext(false).WithBlockHeight(1)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	testdata.RegisterInterfaces(enc.InterfaceRegistry)
	clientCtx := client.Context{}.WithTxConfig(enc.TxConfig).WithCmdContext(context.Background())

	priv := secp256k1.GenPrivKey()
	signer := sdk.AccAddress(priv.PubKey().Address())
	acc := testApp.AccountKeeper.NewAccountWithAddress(ctx, signer)
	testApp.AccountKeeper.SetAccount(ctx, acc)

	txBuilder := clientCtx.TxConfig.NewTxBuilder()
	require.NoError(t, txBuilder.SetMsgs(testdata.NewTestMsg(signer)))
	txBuilder.SetGasLimit(100000)

	if includeExt {
		ext, err := txethereum.NewExtensionOptions(txethereum.SchemaVersion, 12345, []byte{1})
		require.NoError(t, err)
		extBuilder, ok := txBuilder.(authtx.ExtensionOptionsTxBuilder)
		require.True(t, ok)
		extBuilder.SetExtensionOptions(ext)
	}

	signMode := signingtypes.SignMode_SIGN_MODE_DIRECT
	if includeEthereumSignMode {
		signMode = txethereum.SignMode
	}
	require.NoError(t, txBuilder.SetSignatures(signingtypes.SignatureV2{
		PubKey: priv.PubKey(),
		Data: &signingtypes.SingleSignatureData{
			SignMode:  signMode,
			Signature: []byte("placeholder"),
		},
		Sequence: 0,
	}))

	return testApp, ctx, txBuilder.GetTx()
}

func fundAccount(t *testing.T, testApp *app.App, ctx sdk.Context, addr sdk.AccAddress, coins sdk.Coins) {
	t.Helper()
	require.NoError(t, testApp.BankKeeper.MintCoins(ctx, minttypes.ModuleName, coins))
	require.NoError(t, testApp.BankKeeper.SendCoinsFromModuleToAccount(ctx, minttypes.ModuleName, addr, coins))
}

type recordingEthIdentityKeeper struct {
	calls int
}

func (k *recordingEthIdentityKeeper) IndexPubKey(_ sdk.Context, _ cryptotypes.PubKey) error {
	k.calls++
	return nil
}

func (k *recordingEthIdentityKeeper) Resolve(_ sdk.Context, _ []byte) (sdk.AccAddress, bool) {
	return nil, false
}

type signerOnlyTx struct {
	signers [][]byte
}

func (tx signerOnlyTx) GetMsgs() []sdk.Msg {
	return nil
}

func (tx signerOnlyTx) GetMsgsV2() ([]protov2.Message, error) {
	return nil, nil
}

func (tx signerOnlyTx) GetSigners() ([][]byte, error) {
	return tx.signers, nil
}

func (tx signerOnlyTx) GetPubKeys() ([]cryptotypes.PubKey, error) {
	return nil, nil
}

func (tx signerOnlyTx) GetSignaturesV2() ([]signingtypes.SignatureV2, error) {
	return nil, nil
}

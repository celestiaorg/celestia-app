package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/celestiaorg/celestia-app/x/payment/types"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/spf13/cast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/spm/cosmoscmd"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	dbm "github.com/tendermint/tm-db"
)

const testingKeyAcc = "test"

// Get flags every time the simulator is run
func init() {
	simapp.GetSimulatorFlags()
}

func TestProcessMsg(t *testing.T) {
	kb := keyring.NewInMemory()
	info, _, err := kb.NewMnemonic(testingKeyAcc, keyring.English, "", "", hd.Secp256k1)
	if err != nil {
		t.Error(err)
	}
	ns := []byte{1, 1, 1, 1, 1, 1, 1, 1}
	message := bytes.Repeat([]byte{1}, 256)

	// create a signed MsgWirePayFroMessage
	msg := generateSignedWirePayForMessage(t, types.SquareSize, ns, message, kb)

	testApp := setupApp(t, info.GetPubKey())

	tests := []struct {
		name string
		args sdk.Msg
		want core.Message
	}{
		{
			name: "basic",
			args: msg,
			want: core.Message{NamespaceId: msg.MessageNameSpaceId, Data: msg.Message},
		},
	}
	for _, tt := range tests {
		result, _, err := testApp.processMsg(tt.args)
		if err != nil {
			t.Error(err)
		}
		assert.Equal(t, tt.want, result, tt.name)
	}
}

func TestPreprocessTxs(t *testing.T) {
	kb := keyring.NewInMemory()
	info, _, err := kb.NewMnemonic(testingKeyAcc, keyring.English, "", "", hd.Secp256k1)
	if err != nil {
		t.Error(err)
	}

	testApp := setupApp(t, info.GetPubKey())

	type test struct {
		input            abci.RequestPreprocessTxs
		expectedMessages []*core.Message
		expectedTxs      int
	}

	firstNS := []byte{2, 2, 2, 2, 2, 2, 2, 2}
	firstMessage := bytes.Repeat([]byte{2}, 512)
	firstRawTx := generateRawTx(t, testApp.txConfig, firstNS, firstMessage, kb)

	secondNS := []byte{1, 1, 1, 1, 1, 1, 1, 1}
	secondMessage := []byte{2}
	secondRawTx := generateRawTx(t, testApp.txConfig, secondNS, secondMessage, kb)

	thirdNS := []byte{3, 3, 3, 3, 3, 3, 3, 3}
	thirdMessage := []byte{}
	thirdRawTx := generateRawTx(t, testApp.txConfig, thirdNS, thirdMessage, kb)

	tests := []test{
		{
			input: abci.RequestPreprocessTxs{
				Txs: [][]byte{firstRawTx, secondRawTx, thirdRawTx},
			},
			expectedMessages: []*core.Message{
				{
					NamespaceId: secondNS,                                           // the second message should be first
					Data:        append([]byte{2}, bytes.Repeat([]byte{0}, 255)...), // check that the message is padded
				},
				{
					NamespaceId: firstNS,
					Data:        firstMessage,
				},
				{
					NamespaceId: thirdNS,
					Data:        nil,
				},
			},
			expectedTxs: 3,
		},
	}

	for _, tt := range tests {
		res := testApp.PreprocessTxs(tt.input)
		assert.Equal(t, tt.expectedMessages, res.Messages.MessagesList)
		assert.Equal(t, tt.expectedTxs, len(res.Txs))
	}
}

func setupApp(t *testing.T, pub cryptotypes.PubKey) *App {
	// var cache sdk.MultiStorePersistentCache
	// EmptyAppOptions is a stub implementing AppOptions
	emptyOpts := emptyAppOptions{}
	var anteOpt = func(bapp *baseapp.BaseApp) { bapp.SetAnteHandler(nil) }
	db := dbm.NewMemDB()
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stderr))

	skipUpgradeHeights := make(map[int64]bool)

	encCfg := cosmoscmd.MakeEncodingConfig(ModuleBasics)

	testApp := New(
		logger, db, nil, true, skipUpgradeHeights,
		cast.ToString(emptyOpts.Get(flags.FlagHome)),
		cast.ToUint(emptyOpts.Get(server.FlagInvCheckPeriod)),
		encCfg,
		emptyOpts,
		anteOpt,
	)

	genesisState := NewDefaultGenesisState(encCfg.Marshaler)

	genesisState, err := addGenesisAccount(sdk.AccAddress(pub.Address().Bytes()), genesisState, encCfg.Marshaler)
	if err != nil {
		t.Error(err)
	}

	stateBytes, err := json.MarshalIndent(genesisState, "", "  ")
	require.NoError(t, err)

	// Initialize the chain
	testApp.InitChain(
		abci.RequestInitChain{
			Validators:    []abci.ValidatorUpdate{},
			AppStateBytes: stateBytes,
		},
	)

	return testApp
}

type emptyAppOptions struct{}

// Get implements AppOptions
func (ao emptyAppOptions) Get(o string) interface{} {
	return nil
}

// addGenesisAccount mimics the cli addGenesisAccount command, providing an
// account with an allocation of to "token" and "stake" tokens in the genesis
// state
func addGenesisAccount(addr sdk.AccAddress, appState map[string]json.RawMessage, cdc codec.Codec) (map[string]json.RawMessage, error) {
	// create concrete account type based on input parameters
	var genAccount authtypes.GenesisAccount

	coins := sdk.Coins{
		sdk.NewCoin("token", sdk.NewInt(1000000)),
		sdk.NewCoin("stake", sdk.NewInt(1000000)),
	}

	balances := banktypes.Balance{Address: addr.String(), Coins: coins.Sort()}
	baseAccount := authtypes.NewBaseAccount(addr, nil, 0, 0)

	genAccount = baseAccount

	if err := genAccount.Validate(); err != nil {
		return appState, fmt.Errorf("failed to validate new genesis account: %w", err)
	}

	authGenState := authtypes.GetGenesisStateFromAppState(cdc, appState)

	accs, err := authtypes.UnpackAccounts(authGenState.Accounts)
	if err != nil {
		return appState, fmt.Errorf("failed to get accounts from any: %w", err)
	}

	if accs.Contains(addr) {
		return appState, fmt.Errorf("cannot add account at existing address %s", addr)
	}

	// Add the new account to the set of genesis accounts and sanitize the
	// accounts afterwards.
	accs = append(accs, genAccount)
	accs = authtypes.SanitizeGenesisAccounts(accs)

	genAccs, err := authtypes.PackAccounts(accs)
	if err != nil {
		return appState, fmt.Errorf("failed to convert accounts into any's: %w", err)
	}
	authGenState.Accounts = genAccs

	authGenStateBz, err := cdc.MarshalJSON(&authGenState)
	if err != nil {
		return appState, fmt.Errorf("failed to marshal auth genesis state: %w", err)
	}

	appState[authtypes.ModuleName] = authGenStateBz

	bankGenState := banktypes.GetGenesisStateFromAppState(cdc, appState)
	bankGenState.Balances = append(bankGenState.Balances, balances)
	bankGenState.Balances = banktypes.SanitizeGenesisBalances(bankGenState.Balances)

	bankGenStateBz, err := cdc.MarshalJSON(bankGenState)
	if err != nil {
		return appState, fmt.Errorf("failed to marshal bank genesis state: %w", err)
	}

	appState[banktypes.ModuleName] = bankGenStateBz
	return appState, nil
}

func generateRawTx(t *testing.T, txConfig client.TxConfig, ns, message []byte, ring keyring.Keyring) (rawTx []byte) {
	// create a msg
	msg := generateSignedWirePayForMessage(t, types.SquareSize, ns, message, ring)

	info, err := ring.Key(testingKeyAcc)
	if err != nil {
		t.Error(err)
	}

	// this is returning a tx.wrapper
	builder := txConfig.NewTxBuilder()
	err = builder.SetMsgs(msg)
	if err != nil {
		t.Error(err)
	}

	coin := sdk.Coin{
		Denom:  "token",
		Amount: sdk.NewInt(1000),
	}

	builder.SetFeeAmount(sdk.NewCoins(coin))
	builder.SetGasLimit(10000)
	builder.SetTimeoutHeight(99)

	signingData := authsigning.SignerData{
		ChainID:       "test-chain",
		AccountNumber: 0,
		Sequence:      0,
	}

	// Important set the Signature to nil BEFORE actually signing
	sigData := signing.SingleSignatureData{
		SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
		Signature: nil,
	}

	sig := signing.SignatureV2{
		PubKey:   info.GetPubKey(),
		Data:     &sigData,
		Sequence: 0,
	}

	// set the empty signature
	err = builder.SetSignatures(sig)
	if err != nil {
		if err != nil {
			t.Error(err)
		}
	}

	// Generate the bytes to be signed.
	bytesToSign, err := txConfig.
		SignModeHandler().
		GetSignBytes(
			signing.SignMode_SIGN_MODE_DIRECT,
			signingData,
			builder.GetTx(),
		)
	if err != nil {
		t.Error(err)
	}

	// Sign those bytes
	sigBytes, _, err := ring.Sign(testingKeyAcc, bytesToSign)
	if err != nil {
		t.Error(err)
	}

	// Construct the SignatureV2 struct
	sigData = signing.SingleSignatureData{
		SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
		Signature: sigBytes,
	}

	sigV2 := signing.SignatureV2{
		PubKey:   info.GetPubKey(),
		Data:     &sigData,
		Sequence: 0,
	}

	// set the actual signature
	err = builder.SetSignatures(sigV2)
	if err != nil {
		if err != nil {
			t.Error(err)
		}
	}

	// finish the tx
	tx := builder.GetTx()

	// encode the tx
	rawTx, err = txConfig.TxEncoder()(tx)
	if err != nil {
		t.Error(err)
	}

	return rawTx
}

func generateSignedWirePayForMessage(t *testing.T, k uint64, ns, message []byte, ring keyring.Keyring) *types.MsgWirePayForMessage {
	info, err := ring.Key(testingKeyAcc)
	if err != nil {
		t.Error(err)
	}

	msg, err := types.NewMsgWirePayForMessage(ns, message, info.GetPubKey().Bytes(), &types.TransactionFee{}, k)
	if err != nil {
		t.Error(err)
	}

	err = msg.SignShareCommitments(testingKeyAcc, ring)
	if err != nil {
		t.Error(err)
	}

	return msg
}

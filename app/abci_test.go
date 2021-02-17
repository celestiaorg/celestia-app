package app

import (
	"bytes"
	"encoding/json"
	"fmt"
	"os"
	"testing"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client/flags"
	cliTx "github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/lazyledger/lazyledger-app/x/lazyledgerapp/types"
	abci "github.com/lazyledger/lazyledger-core/abci/types"
	"github.com/lazyledger/lazyledger-core/libs/log"
	core "github.com/lazyledger/lazyledger-core/proto/tendermint/types"
	"github.com/spf13/cast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	dbm "github.com/tendermint/tm-db"
)

// TODO:
// - flesh tests with more cases
// 		- test for when the tx should fail

// Get flags every time the simulator is run
func init() {
	simapp.GetSimulatorFlags()
}

func TestProcessMsg(t *testing.T) {
	key := secp256k1.GenPrivKey()

	testApp := setupApp(t)

	for acc := range maccPerms {
		require.Equal(t, !allowedReceivingModAcc[acc], testApp.BankKeeper.BlockedAddr(testApp.AccountKeeper.GetModuleAddress(acc)),
			"ensure that blocked addresses are properly set in bank keeper")
	}

	genesisState := NewDefaultGenesisState()

	genesisState, err := addGenesisAccount(sdk.AccAddress(key.PubKey().Address().Bytes()), genesisState, testApp.appCodec)
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

	// create a tx
	msg := generateWirePayForMessage(t, testApp.SquareSize(), key)

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

// this belongs in the lazyledgerapp/simulation package
func TestRunTx(t *testing.T) {
	key := secp256k1.GenPrivKey()

	testApp := setupApp(t)

	for acc := range maccPerms {
		require.Equal(t, !allowedReceivingModAcc[acc], testApp.BankKeeper.BlockedAddr(testApp.AccountKeeper.GetModuleAddress(acc)),
			"ensure that blocked addresses are properly set in bank keeper")
	}

	genesisState := NewDefaultGenesisState()

	// give the key a bunch a coins for testing
	genesisState, err := addGenesisAccount(sdk.AccAddress(key.PubKey().Address().Bytes()), genesisState, testApp.appCodec)
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
			ChainId:       "test-chain",
		},
	)

	// create a msg
	msg := generateWirePayForMessage(t, 64, key)

	// this is returning a tx.wrapper
	builder := testApp.txConfig.NewTxBuilder()
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
		PubKey:   key.PubKey(),
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

	// create the actual signature
	sigV2, err := cliTx.SignWithPrivKey(signing.SignMode_SIGN_MODE_DIRECT, signingData, builder, key, testApp.txConfig, 0)
	if err != nil {
		t.Error(err)
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

	// verify the signature before encoding
	err = authsigning.VerifySignature(key.PubKey(), signingData, sigV2.Data, testApp.txConfig.SignModeHandler(), tx)
	if err != nil {
		t.Error(err)
	}

	rawTx, err := testApp.txConfig.TxEncoder()(tx)
	if err != nil {
		t.Error(err)
	}

	tests := []struct {
		name  string
		input []byte
		want  core.Message
		mode  uint8
	}{
		{
			name:  "basic",
			mode:  3,
			input: rawTx,
			want:  core.Message{NamespaceId: msg.MessageNameSpaceId, Data: msg.Message},
		},
	}

	for _, tt := range tests {
		_, _, err := testApp.TxRunner()(3, tt.input)
		assert.NoError(t, err, "failure to validate and run tx")
	}
}

// this is more of a sanity check
func TestTxSignature(t *testing.T) {
	key := secp256k1.GenPrivKey()

	encConf := MakeEncodingConfig()
	txConf := encConf.TxConfig

	// create a msg
	msg := generateWirePayForMessage(t, 64, key)

	// this is returning a tx.wrapper
	builder := txConf.NewTxBuilder()
	err := builder.SetMsgs(msg)
	if err != nil {
		t.Error(err)
	}

	signingData := authsigning.SignerData{
		ChainID:       "test-chain",
		AccountNumber: 0,
		Sequence:      0,
	}

	sigData := signing.SingleSignatureData{
		SignMode:  signing.SignMode_SIGN_MODE_DIRECT,
		Signature: nil,
	}

	sig := signing.SignatureV2{
		PubKey:   key.PubKey(),
		Data:     &sigData,
		Sequence: 0,
	}

	// set the unsigned signature data (nil) first
	// this is required for SignWithPriveKey to sign properly
	err = builder.SetSignatures(sig)
	if err != nil {
		if err != nil {
			t.Error(err)
		}
	}

	sigV2, err := cliTx.SignWithPrivKey(signing.SignMode_SIGN_MODE_DIRECT, signingData, builder, key, txConf, 0)
	if err != nil {
		t.Error(err)
	}

	err = builder.SetSignatures(sigV2)
	if err != nil {
		if err != nil {
			t.Error(err)
		}
	}

	tx := builder.GetTx()

	err = authsigning.VerifySignature(key.PubKey(), signingData, sigV2.Data, txConf.SignModeHandler(), tx)
	if err != nil {
		t.Error("failure to verify Signature")
	}

	rawTx, err := txConf.TxEncoder()(tx)
	if err != nil {
		t.Error(err)
	}

	stx, err := txConf.TxDecoder()(rawTx)
	if err != nil {
		t.Error(err)
	}

	// verify the signature after decoding
	err = authsigning.VerifySignature(key.PubKey(), signingData, sigV2.Data, txConf.SignModeHandler(), stx)
	if err != nil {
		t.Error(err)
	}
}

/////////////////////////////
//	Setup App
/////////////////////////////

func setupApp(t *testing.T) *App {
	// var cache sdk.MultiStorePersistentCache
	// EmptyAppOptions is a stub implementing AppOptions
	emptyOpts := emptyAppOptions{}
	var anteOpt = func(bapp *baseapp.BaseApp) { bapp.SetAnteHandler(nil) }
	db := dbm.NewMemDB()
	logger := log.NewTMLogger(log.NewSyncWriter(os.Stderr))

	skipUpgradeHeights := make(map[int64]bool)

	return New(
		"test-app", logger, db, nil, true, skipUpgradeHeights,
		cast.ToString(emptyOpts.Get(flags.FlagHome)),
		cast.ToUint(emptyOpts.Get(server.FlagInvCheckPeriod)),
		MakeEncodingConfig(),
		emptyOpts,
		anteOpt,
	)
}

type emptyAppOptions struct{}

// Get implements AppOptions
func (ao emptyAppOptions) Get(o string) interface{} {
	return nil
}

// addGenesisAccount mimics the cli addGenesisAccount command, providing an
// account with an allocation of to "token" and "stake" tokens in the genesis
// state
func addGenesisAccount(addr sdk.AccAddress, appState map[string]json.RawMessage, cdc codec.Marshaler) (map[string]json.RawMessage, error) {
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

/////////////////////////////
//	Generate Txs
/////////////////////////////

func generateWirePayForMessage(t *testing.T, k uint64, key *secp256k1.PrivKey) *types.MsgWirePayForMessage {
	pubKey := key.PubKey()

	message := bytes.Repeat([]byte{2}, 512)
	nsp := []byte{1, 1, 1, 1, 1, 1, 1, 1}

	commit, err := types.CreateCommit(k, nsp, message)
	if err != nil {
		t.Error(err)
	}

	msg := &types.MsgWirePayForMessage{
		Fee:                &types.TransactionFee{},
		Nonce:              0,
		MessageNameSpaceId: nsp,
		MessageSize:        512,
		Message:            message,
		PublicKey:          pubKey.Bytes(),
		MessageShareCommitment: []types.ShareCommitAndSignature{
			{
				K:               k,
				ShareCommitment: commit,
			},
		},
	}

	rawTxPFM, err := msg.GetCommitmentSignBytes(k)
	if err != nil {
		t.Error(err)
	}

	signedTxPFM, err := key.Sign(rawTxPFM)
	if err != nil {
		t.Error(err)
	}

	msg.MessageShareCommitment[0].Signature = signedTxPFM

	return msg
}

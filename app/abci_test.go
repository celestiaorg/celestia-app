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
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/spf13/cast"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/spm/cosmoscmd"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/pkg/consts"
	core "github.com/tendermint/tendermint/proto/tendermint/types"
	dbm "github.com/tendermint/tm-db"
)

const testingKeyAcc = "test"

// Get flags every time the simulator is run
func init() {
	simapp.GetSimulatorFlags()
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

	genesisState := newDefaultGenesisState(encCfg.Marshaler)

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
// account with an allocation of to "token" and "celes" tokens in the genesis
// state
func addGenesisAccount(addr sdk.AccAddress, appState map[string]json.RawMessage, cdc codec.Codec) (map[string]json.RawMessage, error) {
	// create concrete account type based on input parameters
	var genAccount authtypes.GenesisAccount

	coins := sdk.Coins{
		sdk.NewCoin("token", sdk.NewInt(1000000)),
		sdk.NewCoin(BondDenom, sdk.NewInt(1000000)),
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
	msg := generateSignedWirePayForMessage(t, consts.MaxSquareSize, ns, message, ring)

	krs := generateKeyringSigner(t, "test")
	builder := krs.NewTxBuilder()

	coin := sdk.Coin{
		Denom:  "token",
		Amount: sdk.NewInt(1000),
	}

	builder.SetFeeAmount(sdk.NewCoins(coin))
	builder.SetGasLimit(10000)
	builder.SetTimeoutHeight(99)

	tx, err := krs.BuildSignedTx(builder, msg)
	require.NoError(t, err)

	// encode the tx
	rawTx, err = txConfig.TxEncoder()(tx)
	require.NoError(t, err)

	return rawTx
}

func generateSignedWirePayForMessage(t *testing.T, k uint64, ns, message []byte, ring keyring.Keyring) *types.MsgWirePayForMessage {
	signer := generateKeyringSigner(t, "test")

	msg, err := types.NewWirePayForMessage(ns, message, k)
	if err != nil {
		t.Error(err)
	}

	err = msg.SignShareCommitments(signer)
	if err != nil {
		t.Error(err)
	}

	return msg
}

func generateKeyring(t *testing.T, accts ...string) keyring.Keyring {
	t.Helper()
	kb := keyring.NewInMemory()

	for _, acc := range accts {
		_, _, err := kb.NewMnemonic(acc, keyring.English, "", "", hd.Secp256k1)
		if err != nil {
			t.Error(err)
		}
	}

	_, err := kb.NewAccount(testAccName, testMnemo, "1234", "", hd.Secp256k1)
	if err != nil {
		panic(err)
	}

	return kb
}

func generateKeyringSigner(t *testing.T, accts ...string) *types.KeyringSigner {
	kr := generateKeyring(t, accts...)
	return types.NewKeyringSigner(kr, testAccName, testChainID)
}

const (
	// nolint:lll
	testMnemo   = `ramp soldier connect gadget domain mutual staff unusual first midnight iron good deputy wage vehicle mutual spike unlock rocket delay hundred script tumble choose`
	testAccName = "test-account"
	testChainID = "test-chain-1"
)

// newDefaultGenesisState generates the default state for the application.
func newDefaultGenesisState(cdc codec.JSONCodec) GenesisState {
	return ModuleBasics.DefaultGenesis(cdc)
}

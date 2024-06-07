package util

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/cosmos/cosmos-sdk/client/flags"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/simapp"
	"github.com/cosmos/cosmos-sdk/testutil/mock"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/spf13/cast"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmtypes "github.com/tendermint/tendermint/types"
	dbm "github.com/tendermint/tm-db"

	"github.com/celestiaorg/celestia-app/test/util/genesis"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/tendermint/tendermint/crypto/ed25519"
	tmversion "github.com/tendermint/tendermint/proto/tendermint/version"
)

const (
	ChainID = "test-app"
)

// Get flags every time the simulator is run
func init() {
	simapp.GetSimulatorFlags()
}

type EmptyAppOptions struct{}

// Get implements AppOptions
func (ao EmptyAppOptions) Get(_ string) interface{} {
	return nil
}

// NewTestApp initializes a new app with a no-op logger and in-memory database.
func NewTestApp() *app.App {
	// EmptyAppOptions is a stub implementing AppOptions
	emptyOpts := EmptyAppOptions{}
	// var anteOpt = func(bapp *baseapp.BaseApp) { bapp.SetAnteHandler(nil) }
	db := dbm.NewMemDB()

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	return app.New(
		log.NewNopLogger(),
		db,
		nil,
		true,
		nil,
		"",
		cast.ToUint(emptyOpts.Get(server.FlagInvCheckPeriod)),
		encCfg,
		emptyOpts,
	)
}

// ApplyGenesisState applies genesis with a specified state on initialized testApp.
func ApplyGenesisState(testApp *app.App, pubKeys []cryptotypes.PubKey, balance int64, cparams *tmproto.ConsensusParams) (keyring.Keyring, []genesis.Account, error) {
	// Create genesis
	gen := genesis.NewDefaultGenesis().WithChainID(ChainID).WithConsensusParams(cparams).WithGenesisTime(time.Date(2023, 1, 1, 1, 1, 1, 1, time.UTC).UTC())

	// Add accounts to the genesis state
	for _, pk := range pubKeys {
		err := gen.AddAccount(genesis.Account{
			PubKey:  pk,
			Balance: balance,
		})
		if err != nil {
			return nil, nil, err
		}
	}

	// Hardcoding keys in order to make validator creation deterministic
	consensusKey := ed25519.PrivKey(ed25519.GenPrivKeyFromSecret([]byte("12345678901234567890123456389012")))
	networkKey := ed25519.PrivKey(ed25519.GenPrivKeyFromSecret([]byte("12345678901234567890123456786012")))

	// Add validator to genesis
	err := gen.AddValidator(genesis.Validator{
		KeyringAccount: genesis.KeyringAccount{
			Name:          "validator1",
			InitialTokens: 1_000_000_000,
		},
		Stake:        1_000_000,
		ConsensusKey: consensusKey,
		NetworkKey:   networkKey,
	})
	if err != nil {
		return nil, nil, err
	}

	genDoc, err := gen.Export()
	if err != nil {
		return nil, nil, err
	}

	testApp.Info(abci.RequestInfo{})

	abciParams := &abci.ConsensusParams{
		Block: &abci.BlockParams{
			// choose some value large enough to not bottleneck the max square
			// size
			MaxBytes: int64(appconsts.DefaultSquareSizeUpperBound*appconsts.DefaultSquareSizeUpperBound) * appconsts.ContinuationSparseShareContentSize,
			MaxGas:   cparams.Block.MaxGas,
		},
		Evidence:  &cparams.Evidence,
		Validator: &cparams.Validator,
		Version:   &cparams.Version,
	}

	// Init chain will set the validator set and initialize the genesis accounts
	// TODO: Understand why genDoc.GenesisTime is getting reset
	testApp.InitChain(
		abci.RequestInitChain{
			Time:            gen.GenesisTime,
			Validators:      []abci.ValidatorUpdate{},
			ConsensusParams: abciParams,
			AppStateBytes:   genDoc.AppState,
			ChainId:         genDoc.ChainID,
		},
	)

	// Commit genesis changes
	testApp.Commit()
	testApp.BeginBlock(abci.RequestBeginBlock{Header: tmproto.Header{
		ChainID:            ChainID,
		Height:             testApp.LastBlockHeight() + 1,
		AppHash:            testApp.LastCommitID().Hash,
		ValidatorsHash:     genDoc.ValidatorHash(),
		NextValidatorsHash: genDoc.ValidatorHash(),
		Version: tmversion.Consensus{
			App: cparams.Version.AppVersion,
		},
	}})

	return gen.Keyring(), gen.Accounts(), nil
}

// SetupTestAppWithGenesisValSet initializes a new app with a validator set and
// genesis accounts that also act as delegators. For simplicity, each validator
// is bonded with a delegation of one consensus engine unit in the default token
// of the app from first genesis account. A no-op logger is set in app.
func SetupTestAppWithGenesisValSet(cparams *tmproto.ConsensusParams, genAccounts ...string) (*app.App, keyring.Keyring) {
	emptyOptions := emptyAppOptions{}
	skipUpgradeHeights := make(map[int64]bool)
	encodingConfig := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	testApp := app.New(
		log.NewNopLogger(),
		dbm.NewMemDB(),
		nil,
		true,
		skipUpgradeHeights,
		cast.ToString(emptyOptions.Get(flags.FlagHome)),
		cast.ToUint(emptyOptions.Get(server.FlagInvCheckPeriod)),
		encodingConfig,
		emptyOptions,
	)

	genesisState, valSet, kr := GenesisStateWithSingleValidator(testApp, genAccounts...)

	stateBytes, err := json.MarshalIndent(genesisState, "", " ")
	if err != nil {
		panic(err)
	}

	abciParams := &abci.ConsensusParams{
		Block: &abci.BlockParams{
			// choose some value large enough to not bottleneck the max square
			// size
			MaxBytes: int64(appconsts.DefaultSquareSizeUpperBound*appconsts.DefaultSquareSizeUpperBound) * appconsts.ContinuationSparseShareContentSize,
			MaxGas:   cparams.Block.MaxGas,
		},
		Evidence:  &cparams.Evidence,
		Validator: &cparams.Validator,
		Version:   &cparams.Version,
	}

	genesisTime := time.Date(2023, 1, 1, 1, 1, 1, 1, time.UTC).UTC()

	// init chain will set the validator set and initialize the genesis accounts
	testApp.InitChain(
		abci.RequestInitChain{
			Time:            genesisTime,
			Validators:      []abci.ValidatorUpdate{},
			ConsensusParams: abciParams,
			AppStateBytes:   stateBytes,
			ChainId:         ChainID,
		},
	)

	// commit genesis changes
	testApp.Commit()
	testApp.BeginBlock(abci.RequestBeginBlock{Header: tmproto.Header{
		Height:             testApp.LastBlockHeight() + 1,
		AppHash:            testApp.LastCommitID().Hash,
		ValidatorsHash:     valSet.Hash(),
		NextValidatorsHash: valSet.Hash(),
	}})

	return testApp, kr
}

// AddGenesisAccount mimics the cli addGenesisAccount command, providing an
// account with an allocation of to "token" and "tia" tokens in the genesis
// state
func AddGenesisAccount(addr sdk.AccAddress, appState app.GenesisState, cdc codec.Codec) (map[string]json.RawMessage, error) {
	// create concrete account type based on input parameters
	var genAccount authtypes.GenesisAccount

	coins := sdk.Coins{
		sdk.NewCoin("token", sdk.NewInt(1000000)),
		sdk.NewCoin(app.BondDenom, sdk.NewInt(1000000)),
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

// GenesisStateWithSingleValidator initializes GenesisState with a single
// validator and genesis accounts that also act as delegators.
func GenesisStateWithSingleValidator(testApp *app.App, genAccounts ...string) (app.GenesisState, *tmtypes.ValidatorSet, keyring.Keyring) {
	privVal := mock.NewPV()
	pubKey, err := privVal.GetPubKey()
	if err != nil {
		panic(err)
	}

	// create validator set with single validator
	validator := tmtypes.NewValidator(pubKey, 1)
	valSet := tmtypes.NewValidatorSet([]*tmtypes.Validator{validator})

	// generate genesis account
	senderPrivKey := secp256k1.GenPrivKey()
	accs := make([]authtypes.GenesisAccount, 0, len(genAccounts)+1)
	acc := authtypes.NewBaseAccount(senderPrivKey.PubKey().Address().Bytes(), senderPrivKey.PubKey(), 0, 0)
	accs = append(accs, acc)
	balances := make([]banktypes.Balance, 0, len(genAccounts)+1)
	balances = append(balances, banktypes.Balance{
		Address: acc.GetAddress().String(),
		Coins:   sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(100000000000000))),
	})

	kr, fundedBankAccs, fundedAuthAccs := testfactory.FundKeyringAccounts(genAccounts...)
	accs = append(accs, fundedAuthAccs...)
	balances = append(balances, fundedBankAccs...)

	genesisState := NewDefaultGenesisState(testApp.AppCodec())
	genesisState = genesisStateWithValSet(testApp, genesisState, valSet, accs, balances...)

	return genesisState, valSet, kr
}

func genesisStateWithValSet(
	a *app.App,
	genesisState app.GenesisState,
	valSet *tmtypes.ValidatorSet,
	genAccs []authtypes.GenesisAccount,
	balances ...banktypes.Balance,
) app.GenesisState {
	// set genesis accounts
	authGenesis := authtypes.NewGenesisState(authtypes.DefaultParams(), genAccs)
	genesisState[authtypes.ModuleName] = a.AppCodec().MustMarshalJSON(authGenesis)

	validators := make([]stakingtypes.Validator, 0, len(valSet.Validators))
	delegations := make([]stakingtypes.Delegation, 0, len(valSet.Validators))

	bondAmt := sdk.DefaultPowerReduction

	for _, val := range valSet.Validators {
		pk, err := cryptocodec.FromTmPubKeyInterface(val.PubKey)
		if err != nil {
			panic(err)
		}
		pkAny, err := codectypes.NewAnyWithValue(pk)
		if err != nil {
			panic(err)
		}
		validator := stakingtypes.Validator{
			OperatorAddress:   sdk.ValAddress(val.Address).String(),
			ConsensusPubkey:   pkAny,
			Jailed:            false,
			Status:            stakingtypes.Bonded,
			Tokens:            bondAmt,
			DelegatorShares:   sdk.OneDec(),
			Description:       stakingtypes.Description{},
			UnbondingHeight:   int64(0),
			UnbondingTime:     time.Unix(0, 0).UTC(),
			Commission:        stakingtypes.NewCommission(sdk.ZeroDec(), sdk.ZeroDec(), sdk.ZeroDec()),
			MinSelfDelegation: sdk.ZeroInt(),
		}
		validators = append(validators, validator)
		delegations = append(delegations, stakingtypes.NewDelegation(genAccs[0].GetAddress(), val.Address.Bytes(), sdk.OneDec()))

	}
	// set validators and delegations
	params := stakingtypes.DefaultParams()
	params.BondDenom = app.BondDenom
	stakingGenesis := stakingtypes.NewGenesisState(params, validators, delegations)
	genesisState[stakingtypes.ModuleName] = a.AppCodec().MustMarshalJSON(stakingGenesis)

	totalSupply := sdk.NewCoins()
	for _, b := range balances {
		// add genesis acc tokens to total supply
		totalSupply = totalSupply.Add(b.Coins...)
	}

	for range delegations {
		// add delegated tokens to total supply
		totalSupply = totalSupply.Add(sdk.NewCoin(app.BondDenom, bondAmt))
	}

	// add bonded amount to bonded pool module account
	balances = append(balances, banktypes.Balance{
		Address: authtypes.NewModuleAddress(stakingtypes.BondedPoolName).String(),
		Coins:   sdk.Coins{sdk.NewCoin(app.BondDenom, bondAmt)},
	})

	// update total supply
	bankGenesis := banktypes.NewGenesisState(banktypes.DefaultGenesisState().Params, balances, totalSupply, []banktypes.Metadata{})
	genesisState[banktypes.ModuleName] = a.AppCodec().MustMarshalJSON(bankGenesis)

	return genesisState
}

// NewDefaultGenesisState generates the default state for the application.
func NewDefaultGenesisState(cdc codec.JSONCodec) app.GenesisState {
	return app.ModuleBasics.DefaultGenesis(cdc)
}

package util

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	v1 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v1"
	v2 "github.com/celestiaorg/celestia-app/v2/pkg/appconsts/v2"
	"github.com/celestiaorg/celestia-app/v2/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v2/test/util/testfactory"
	"github.com/celestiaorg/celestia-app/v2/test/util/testnode"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/spf13/cast"
	"github.com/stretchr/testify/require"
	abci "github.com/tendermint/tendermint/abci/types"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/libs/log"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	tmversion "github.com/tendermint/tendermint/proto/tendermint/version"
	tmtypes "github.com/tendermint/tendermint/types"
	dbm "github.com/tendermint/tm-db"

	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

const ChainID = testfactory.ChainID

// Get flags every time the simulator is run
func init() {
	simapp.GetSimulatorFlags()
}

type EmptyAppOptions struct{}

// Get implements AppOptions
func (ao EmptyAppOptions) Get(_ string) interface{} {
	return nil
}

// SetupTestAppWithGenesisValSet initializes a new app with a validator set and
// genesis accounts that also act as delegators. For simplicity, each validator
// is bonded with a delegation of one consensus engine unit in the default token
// of the app from first genesis account. A no-op logger is set in app.
func SetupTestAppWithGenesisValSet(cparams *tmproto.ConsensusParams, genAccounts ...string) (*app.App, keyring.Keyring) {
	testApp, valSet, kr := NewTestAppWithGenesisSet(cparams, genAccounts...)

	// commit genesis changes
	testApp.Commit()
	testApp.BeginBlock(abci.RequestBeginBlock{Header: tmproto.Header{
		ChainID:            ChainID,
		Height:             testApp.LastBlockHeight() + 1,
		AppHash:            testApp.LastCommitID().Hash,
		ValidatorsHash:     valSet.Hash(),
		NextValidatorsHash: valSet.Hash(),
		Version: tmversion.Consensus{
			App: cparams.Version.AppVersion,
		},
	}})

	return testApp, kr
}

// NewTestApp creates a new app instance with an empty memDB and a no-op logger.
func NewTestApp() *app.App {
	// EmptyAppOptions is a stub implementing AppOptions
	emptyOpts := EmptyAppOptions{}
	// var anteOpt = func(bapp *baseapp.BaseApp) { bapp.SetAnteHandler(nil) }
	db := dbm.NewMemDB()

	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)

	return app.New(
		log.NewNopLogger(), db, nil,
		cast.ToUint(emptyOpts.Get(server.FlagInvCheckPeriod)),
		encCfg,
		0,
		emptyOpts,
	)
}

// SetupDeterministicGenesisState sets genesis on initialized testApp with the provided arguments.
func SetupDeterministicGenesisState(testApp *app.App, pubKeys []cryptotypes.PubKey, balance int64, cparams *tmproto.ConsensusParams) (keyring.Keyring, []genesis.Account, error) {
	// create genesis
	gen := genesis.NewDefaultGenesis().
		WithChainID(ChainID).
		WithConsensusParams(cparams).
		WithGenesisTime(time.Date(2023, 1, 1, 1, 1, 1, 1, time.UTC).UTC())

	// add accounts to genesis
	for _, pk := range pubKeys {
		err := gen.AddAccount(genesis.Account{
			PubKey:  pk,
			Balance: balance,
		})
		if err != nil {
			return nil, nil, err
		}
	}

	// add validator to genesis
	err := AddDeterministicValidatorsToGenesis(gen)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to add validator: %w", err)
	}

	genDoc, err := gen.Export()
	if err != nil {
		return nil, nil, fmt.Errorf("failed to export genesis doc: %w", err)
	}

	// initialise test app against genesis
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

	// init chain will set the validator set and initialize the genesis accounts
	testApp.InitChain(
		abci.RequestInitChain{
			Time:            gen.GenesisTime,
			Validators:      []abci.ValidatorUpdate{},
			ConsensusParams: abciParams,
			AppStateBytes:   genDoc.AppState,
			ChainId:         genDoc.ChainID,
		},
	)

	// commit genesis changes
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

// NewTestAppWithGenesisSet initializes a new app with genesis accounts and returns the testApp, validator set and keyring.
func NewTestAppWithGenesisSet(cparams *tmproto.ConsensusParams, genAccounts ...string) (*app.App, *tmtypes.ValidatorSet, keyring.Keyring) {
	testApp := NewTestApp()
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

	testApp.Info(abci.RequestInfo{})

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
	return testApp, valSet, kr
}

// AddDeterministicValidatorToGenesis adds a single deterministic validator to the genesis.
func AddDeterministicValidatorsToGenesis(g *genesis.Genesis) error {
	for i := range FixedMnemonics {
		val := genesis.Validator{
			KeyringAccount: genesis.KeyringAccount{
				Name:          "validator" + fmt.Sprint(i),
				InitialTokens: 1_000_000_000,
			},
			Stake:        1_000_000,
			ConsensusKey: FixedConsensusPrivKeys[i],
			NetworkKey:   FixedNetWorkPrivKeys[i],
		}

		// initialize the validator's genesis account in the keyring
		rec, err := g.Keyring().NewAccount(val.Name, FixedMnemonics[i], "", "", hd.Secp256k1)
		if err != nil {
			return fmt.Errorf("failed to create account: %w", err)
		}

		validatorPubKey, err := rec.GetPubKey()
		if err != nil {
			return fmt.Errorf("failed to get pubkey: %w", err)
		}

		// make account from keyring account
		account := genesis.Account{
			PubKey:  validatorPubKey,
			Balance: val.KeyringAccount.InitialTokens,
		}

		// add the validator's account to the genesis
		if err := g.AddAccount(account); err != nil {
			return fmt.Errorf("failed to add account: %w", err)
		}
		if err := g.AddValidator(val); err != nil {
			return fmt.Errorf("failed to add validator: %w", err)
		}
	}

	return nil
}

// AddAccount mimics the cli addAccount command, providing an
// account with an allocation of to "token" and "tia" tokens in the genesis
// state
func AddAccount(addr sdk.AccAddress, appState app.GenesisState, cdc codec.Codec) (map[string]json.RawMessage, error) {
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
	// create validator set with single validator
	validatorPubKey := ed25519.PubKey([]byte("12345678901234567890123456789012"))
	validator := tmtypes.NewValidator(validatorPubKey, 1)
	valSet := tmtypes.NewValidatorSet([]*tmtypes.Validator{validator})

	// generate sender account
	senderPrivKey := secp256k1.GenPrivKeyFromSecret([]byte("09876543210987654321098765432109"))
	acc := authtypes.NewBaseAccount(senderPrivKey.PubKey().Address().Bytes(), senderPrivKey.PubKey(), 0, 0)

	// append sender account to genesis accounts
	accs := make([]authtypes.GenesisAccount, 0, len(genAccounts)+1)
	accs = append(accs, acc)

	// genesis accounts and sender account balances
	balances := make([]banktypes.Balance, 0, len(genAccounts)+1)
	balances = append(balances, banktypes.Balance{
		Address: acc.GetAddress().String(),
		Coins:   sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(100000000000000))),
	})

	kr, fundedBankAccs, fundedAuthAccs := testnode.FundKeyringAccounts(genAccounts...)

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

func SetupTestAppWithUpgradeHeight(t *testing.T, upgradeHeight int64) (*app.App, keyring.Keyring) {
	t.Helper()

	db := dbm.NewMemDB()
	chainID := "test_chain"
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	testApp := app.New(log.NewNopLogger(), db, nil, 0, encCfg, upgradeHeight, EmptyAppOptions{})
	genesisState, _, kr := GenesisStateWithSingleValidator(testApp, "account")
	stateBytes, err := json.MarshalIndent(genesisState, "", " ")
	require.NoError(t, err)
	infoResp := testApp.Info(abci.RequestInfo{})
	require.EqualValues(t, 0, infoResp.AppVersion)
	cp := app.DefaultInitialConsensusParams()
	abciParams := &abci.ConsensusParams{
		Block: &abci.BlockParams{
			MaxBytes: cp.Block.MaxBytes,
			MaxGas:   cp.Block.MaxGas,
		},
		Evidence:  &cp.Evidence,
		Validator: &cp.Validator,
		Version:   &cp.Version,
	}

	_ = testApp.InitChain(
		abci.RequestInitChain{
			Time:            time.Now(),
			Validators:      []abci.ValidatorUpdate{},
			ConsensusParams: abciParams,
			AppStateBytes:   stateBytes,
			ChainId:         chainID,
		},
	)

	// assert that the chain starts with version provided in genesis
	infoResp = testApp.Info(abci.RequestInfo{})
	require.EqualValues(t, app.DefaultInitialConsensusParams().Version.AppVersion, infoResp.AppVersion)

	_ = testApp.Commit()
	supportedVersions := []uint64{v1.Version, v2.Version}
	require.Equal(t, supportedVersions, testApp.SupportedVersions())
	return testApp, kr
}

package node

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	sdk "github.com/cosmos/cosmos-sdk/types"
	auth "github.com/cosmos/cosmos-sdk/x/auth/types"
	bank "github.com/cosmos/cosmos-sdk/x/bank/types"
	slashing "github.com/cosmos/cosmos-sdk/x/slashing/types"
	staking "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/spf13/viper"
	tmconfig "github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/crypto"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/privval"
	"github.com/tendermint/tendermint/types"
)

type Filesystem struct {
	App       *serverconfig.Config
	Consensus *tmconfig.Config
	genesis   *types.GenesisDoc
	signer    *privval.FilePV
	nodeKey   *p2p.NodeKey
}

func NewFilesystem(
	app *serverconfig.Config,
	consensus *tmconfig.Config,
	genesis *types.GenesisDoc,
	signer *privval.FilePV,
	nodeKey *p2p.NodeKey,
) *Filesystem {
	return &Filesystem{
		App:       app,
		Consensus: consensus,
		genesis:   genesis,
		signer:    signer,
		nodeKey:   nodeKey,
	}
}

func DefaultFilesystem(dir string) *Filesystem {
	cfg := app.DefaultConsensusConfig()
	cfg.SetRoot(dir)
	pv := privval.GenFilePV(cfg.PrivValidatorKeyFile(), cfg.PrivValidatorStateFile())
	return &Filesystem{
		App:       app.DefaultAppConfig(),
		Consensus: cfg,
		signer:    pv,
		nodeKey: &p2p.NodeKey{
			PrivKey: ed25519.GenPrivKey(),
		},
	}
}

func Load(dir string) (*Filesystem, error) {
	consensusCfg, err := LoadConsensusConfig(dir)
	if err != nil {
		return nil, err
	}
	appCfg, err := LoadAppConfig(dir)
	if err != nil {
		return nil, err
	}

	return NewFilesystem(appCfg, consensusCfg, nil, nil, nil), nil
}

func (fs *Filesystem) Save() error {
	// save the consensus config, overwriting the existing file
	tmconfig.WriteConfigFile(filepath.Join(fs.Consensus.RootDir, "config", "config.toml"), fs.Consensus)

	// save the application config, overwriting the existing file
	serverconfig.WriteConfigFile(filepath.Join(fs.Consensus.RootDir, "config", "app.toml"), fs.App)

	// if no genesis exists and one is provided, save it (it's not possible to override an
	// existing genesis file)
	genesisFile := filepath.Join(fs.Consensus.RootDir, "config", "genesis.json")
	if !fileExists(genesisFile) && fs.genesis != nil {
		if err := fs.genesis.SaveAs(genesisFile); err != nil {
			return err
		}
	}

	// save the node key
	if !fileExists(fs.Consensus.NodeKeyFile()) && fs.nodeKey != nil {
		if err := fs.nodeKey.SaveAs(fs.Consensus.NodeKeyFile()); err != nil {
			return err
		}
	}

	// save the consensus signer (filePV)
	if fs.signer != nil {
		fs.signer.Save()
	}

	// TODO: add addressbook.json
	return nil
}

func (fs *Filesystem) Genesis() (*types.GenesisDoc, error) {
	var err error
	if fs.genesis == nil {
		fs.genesis, err = types.GenesisDocFromFile(fs.Consensus.GenesisFile())
	}
	return fs.genesis, err
}

func LoadConsensusConfig(dir string) (*tmconfig.Config, error) {
	cfg := app.DefaultConsensusConfig()
	path := filepath.Join(dir, "config")
	viper.AddConfigPath(path)
	viper.SetConfigName("config")
	viper.SetConfigType("toml")
	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, err
	}
	if err := cfg.ValidateBasic(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func LoadAppConfig(dir string) (*serverconfig.Config, error) {
	cfg := app.DefaultAppConfig()
	path := filepath.Join(dir, "config")
	viper.AddConfigPath(path)
	viper.SetConfigName("app")
	viper.SetConfigType("toml")
	if err := viper.ReadInConfig(); err != nil {
		return nil, err
	}
	if err := viper.Unmarshal(cfg); err != nil {
		return nil, err
	}
	if err := cfg.ValidateBasic(); err != nil {
		return nil, err
	}
	return cfg, nil
}

func MakeSingleValidatorGenesis(accountKey, validatorKey crypto.PubKey) (*types.GenesisDoc, error) {
	encCdc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	appGenState := app.ModuleBasics.DefaultGenesis(encCdc.Codec)
	bankGenesis := bank.DefaultGenesisState()
	stakingGenesis := staking.DefaultGenesisState()
	slashingGenesis := slashing.DefaultGenesisState()
	genAccs := []auth.GenesisAccount{}
	stakingGenesis.Params.BondDenom = app.BondDenom

	// setup the validator information on the state machine
	addr := accountKey.Address()
	pk, err := cryptocodec.FromTmPubKeyInterface(validatorKey)
	if err != nil {
		return &types.GenesisDoc{}, fmt.Errorf("converting public key for node: %w", err)
	}
	pkAny, err := codectypes.NewAnyWithValue(pk)
	if err != nil {
		return &types.GenesisDoc{}, err
	}

	validators := []staking.Validator{{
		OperatorAddress: sdk.ValAddress(addr).String(),
		ConsensusPubkey: pkAny,
		Description: staking.Description{
			Moniker: "node",
		},
		Status:          staking.Bonded,
		Tokens:          sdk.NewInt(1_000_000_000),
		DelegatorShares: sdk.OneDec(),
		// 5% commission
		Commission:        staking.NewCommission(sdk.NewDecWithPrec(5, 2), sdk.OneDec(), sdk.OneDec()),
		MinSelfDelegation: sdk.ZeroInt(),
	}}
	consensusAddr := pk.Address()
	delegations := staking.NewDelegation(sdk.AccAddress(addr), sdk.ValAddress(addr), sdk.OneDec())
	valInfo := []slashing.SigningInfo{{
		Address:              sdk.ConsAddress(consensusAddr).String(),
		ValidatorSigningInfo: slashing.NewValidatorSigningInfo(sdk.ConsAddress(consensusAddr), 1, 0, time.Unix(0, 0), false, 0),
	}}
	stakingGenesis.Delegations = []staking.Delegation{delegations}
	stakingGenesis.Validators = validators
	slashingGenesis.SigningInfos = valInfo

	accountPk, err := cryptocodec.FromTmPubKeyInterface(accountKey)
	if err != nil {
		return &types.GenesisDoc{}, fmt.Errorf("converting public key for account: %w", err)
	}

	acc := auth.NewBaseAccount(addr.Bytes(), accountPk, 0, 0)
	genAccs = append(genAccs, acc)
	// add bonded amount to bonded pool module account
	balances := []bank.Balance{
		{
			Address: auth.NewModuleAddress(staking.BondedPoolName).String(),
			Coins:   sdk.Coins{sdk.NewCoin(app.BondDenom, validators[0].Tokens)},
		},
		{
			Address: sdk.AccAddress(addr).String(),
			Coins: sdk.NewCoins(
				sdk.NewCoin(app.BondDenom, sdk.NewInt(1_000_000_000)),
			),
		},
	}
	bankGenesis.Balances = bank.SanitizeGenesisBalances(balances)
	authGenesis := auth.NewGenesisState(auth.DefaultParams(), genAccs)

	// update the original genesis state
	appGenState[bank.ModuleName] = encCdc.Codec.MustMarshalJSON(bankGenesis)
	appGenState[auth.ModuleName] = encCdc.Codec.MustMarshalJSON(authGenesis)
	appGenState[staking.ModuleName] = encCdc.Codec.MustMarshalJSON(stakingGenesis)
	appGenState[slashing.ModuleName] = encCdc.Codec.MustMarshalJSON(slashingGenesis)

	if err := app.ModuleBasics.ValidateGenesis(encCdc.Codec, encCdc.TxConfig, appGenState); err != nil {
		return &types.GenesisDoc{}, fmt.Errorf("validating genesis: %w", err)
	}

	appState, err := json.MarshalIndent(appGenState, "", " ")
	if err != nil {
		return &types.GenesisDoc{}, fmt.Errorf("marshaling app state: %w", err)
	}

	// Validator set and app hash are set in InitChain
	return &types.GenesisDoc{
		ChainID:         "private",
		GenesisTime:     time.Now().UTC(),
		ConsensusParams: types.DefaultConsensusParams(),
		AppState:        appState,
	}, nil
}

func fileExists(file string) bool {
	_, err := os.Stat(file)
	return os.IsExist(err)
}

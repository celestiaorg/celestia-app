package network

import (
	"fmt"
	"testing"
	"time"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/simapp"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	"github.com/cosmos/cosmos-sdk/testutil/network"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/spm/cosmoscmd"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	tmdb "github.com/tendermint/tm-db"

	"github.com/celestiaorg/celestia-app/app"
)

func New(t *testing.T, config network.Config, genAccNames ...string) *network.Network {
	kr := generateKeyring(t)

	// add genesis accounts
	genAuthAccs := make([]authtypes.GenesisAccount, len(genAccNames))
	genBalances := make([]banktypes.Balance, len(genAccNames))
	mnemonics := make([]string, len(genAccNames))
	for i, name := range genAccNames {
		a, b, mnm := newGenAccout(kr, name, 1000000000000)
		genAuthAccs[i] = a
		genBalances[i] = b
		mnemonics[i] = mnm
	}

	config, err := addGenAccounts(config, genAuthAccs, genBalances)
	if err != nil {
		panic(err)
	}

	net := network.New(t, config)

	// add the keys to the keyring that is used by the integration test
	for i, name := range genAccNames {
		_, err := net.Validators[0].ClientCtx.Keyring.NewAccount(name, mnemonics[i], "", "", hd.Secp256k1)
		require.NoError(t, err)
	}

	return net
}

// DefaultConfig will initialize config for the network with custom application,
// genesis and single validator. All other parameters are inherited from cosmos-sdk/testutil/network.DefaultConfig
func DefaultConfig() network.Config {
	encoding := cosmoscmd.MakeEncodingConfig(app.ModuleBasics)
	return network.Config{
		Codec:             encoding.Marshaler,
		TxConfig:          encoding.TxConfig,
		LegacyAmino:       encoding.Amino,
		InterfaceRegistry: encoding.InterfaceRegistry,
		AccountRetriever:  authtypes.AccountRetriever{},
		AppConstructor: func(val network.Validator) servertypes.Application {
			return app.New(
				val.Ctx.Logger, tmdb.NewMemDB(), nil, true, map[int64]bool{}, val.Ctx.Config.RootDir, 0,
				encoding,
				simapp.EmptyAppOptions{},
				baseapp.SetPruning(storetypes.NewPruningOptionsFromString(val.AppConfig.Pruning)),
				baseapp.SetMinGasPrices(val.AppConfig.MinGasPrices),
			)
		},
		GenesisState:    app.ModuleBasics.DefaultGenesis(encoding.Marshaler),
		TimeoutCommit:   2 * time.Second,
		ChainID:         "chain-" + tmrand.NewRand().Str(6),
		NumValidators:   1,
		BondDenom:       app.BondDenom,
		MinGasPrices:    fmt.Sprintf("0.000006%s", app.BondDenom),
		AccountTokens:   sdk.TokensFromConsensusPower(1000, sdk.DefaultPowerReduction),
		StakingTokens:   sdk.TokensFromConsensusPower(500, sdk.DefaultPowerReduction),
		BondedTokens:    sdk.TokensFromConsensusPower(100, sdk.DefaultPowerReduction),
		PruningStrategy: storetypes.PruningOptionNothing,
		CleanupDir:      true,
		SigningAlgo:     string(hd.Secp256k1Type),
		KeyringOptions:  []keyring.Option{},
	}
}

func addGenAccounts(cfg network.Config, genAccounts []authtypes.GenesisAccount, genBalances []banktypes.Balance) (network.Config, error) {
	// set the accounts in the genesis state
	var authGenState authtypes.GenesisState
	cfg.Codec.MustUnmarshalJSON(cfg.GenesisState[authtypes.ModuleName], &authGenState)

	accounts, err := authtypes.PackAccounts(genAccounts)
	if err != nil {
		return cfg, err
	}

	authGenState.Accounts = append(authGenState.Accounts, accounts...)
	cfg.GenesisState[authtypes.ModuleName] = cfg.Codec.MustMarshalJSON(&authGenState)

	// set the balances in the genesis state
	var bankGenState banktypes.GenesisState
	cfg.Codec.MustUnmarshalJSON(cfg.GenesisState[banktypes.ModuleName], &bankGenState)

	bankGenState.Balances = append(bankGenState.Balances, genBalances...)
	cfg.GenesisState[banktypes.ModuleName] = cfg.Codec.MustMarshalJSON(&bankGenState)

	return cfg, nil
}

func newGenAccout(kr keyring.Keyring, name string, amount int64) (authtypes.GenesisAccount, banktypes.Balance, string) {
	info, mnm, err := kr.NewMnemonic(name, keyring.English, "", "", hd.Secp256k1)
	if err != nil {
		panic(err)
	}

	// create coin
	balances := sdk.NewCoins(
		sdk.NewCoin(fmt.Sprintf("%stoken", name), sdk.NewInt(amount)),
		sdk.NewCoin(app.BondDenom, sdk.NewInt(amount)),
	)

	bal := banktypes.Balance{
		Address: info.GetAddress().String(),
		Coins:   balances.Sort(),
	}

	return authtypes.NewBaseAccount(info.GetAddress(), info.GetPubKey(), 0, 0), bal, mnm
}

func generateKeyring(t *testing.T) keyring.Keyring {
	t.Helper()
	kb := keyring.NewInMemory()
	return kb
}

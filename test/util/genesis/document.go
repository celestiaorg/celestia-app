package genesis

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// Document will create a valid genesis doc with funded addresses.
func Document(
	ecfg encoding.Config,
	params *tmproto.ConsensusParams,
	chainID string,
	gentxs []json.RawMessage,
	accounts []Account,
	mods ...Modifier,
) (*coretypes.GenesisDoc, error) {
	genutilGenState := genutiltypes.DefaultGenesisState()
	genutilGenState.GenTxs = gentxs

	genBals, genAccs, err := accountsToSDKTypes(accounts)
	if err != nil {
		return nil, fmt.Errorf("converting accounts into sdk types: %w", err)
	}

	sdkAccounts, err := authtypes.PackAccounts(genAccs)
	if err != nil {
		return nil, fmt.Errorf("packing accounts: %w", err)
	}

	authGenState := authtypes.DefaultGenesisState()
	bankGenState := banktypes.DefaultGenesisState()
	authGenState.Accounts = append(authGenState.Accounts, sdkAccounts...)
	bankGenState.Balances = append(bankGenState.Balances, genBals...)
	bankGenState.Balances = banktypes.SanitizeGenesisBalances(bankGenState.Balances)

	// perform some basic validation of the genesis state
	if err := authtypes.ValidateGenesis(*authGenState); err != nil {
		return nil, err
	}
	if err := bankGenState.Validate(); err != nil {
		return nil, err
	}
	if err := genutiltypes.ValidateGenesis(genutilGenState, ecfg.TxConfig.TxJSONDecoder()); err != nil {
		return nil, err
	}

	state := app.ModuleBasics.DefaultGenesis(ecfg.Codec)
	state[authtypes.ModuleName] = ecfg.Codec.MustMarshalJSON(authGenState)
	state[banktypes.ModuleName] = ecfg.Codec.MustMarshalJSON(bankGenState)
	state[genutiltypes.ModuleName] = ecfg.Codec.MustMarshalJSON(genutilGenState)

	for _, modifier := range mods {
		state = modifier(state)
	}

	stateBz, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, fmt.Errorf("marshalling genesis state: %w", err)
	}

	// Create the genesis doc
	genesisDoc := &coretypes.GenesisDoc{
		ChainID:         chainID,
		GenesisTime:     time.Now(),
		ConsensusParams: params,
		AppState:        stateBz,
	}

	return genesisDoc, nil
}

// accountsToSDKTypes converts the genesis accounts to native SDK types.
func accountsToSDKTypes(accounts []Account) ([]banktypes.Balance, []authtypes.GenesisAccount, error) {
	genBals := make([]banktypes.Balance, len(accounts))
	genAccs := make([]authtypes.GenesisAccount, len(accounts))
	hasMap := make(map[string]struct{})
	for i, account := range accounts {
		if err := account.ValidateBasic(); err != nil {
			return nil, nil, fmt.Errorf("invalid account %d: %v", i, err)
		}
		addr := sdk.AccAddress(account.PubKey.Address())
		if _, ok := hasMap[addr.String()]; ok {
			return nil, nil, fmt.Errorf("duplicate account address %s", addr)
		}
		hasMap[addr.String()] = struct{}{}

		balances := sdk.NewCoins(
			sdk.NewCoin(appconsts.BondDenom, sdk.NewInt(DefaultInitialBalance)),
		)

		genBals[i] = banktypes.Balance{Address: addr.String(), Coins: balances.Sort()}

		genAccs[i] = authtypes.NewBaseAccount(addr, account.PubKey, uint64(i), 0)
	}
	return genBals, genAccs, nil
}

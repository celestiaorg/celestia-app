package genesis

import (
	"encoding/json"
	"time"

	"github.com/celestiaorg/celestia-app/app/encoding"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
)

func NewGenesisRegularAccount(
	address string,
	balances sdk.Coins,
) (account authtypes.GenesisAccount, balance banktypes.Balance, err error) {
	sdkAddr, err := sdk.AccAddressFromBech32(address)
	if err != nil {
		return account, balance, err
	}

	balance = banktypes.Balance{
		Address: address,
		Coins:   balances,
	}
	bAccount := authtypes.NewBaseAccountWithAddress(sdkAddr)

	return authtypes.GenesisAccount(bAccount), balance, nil
}

// NewGenesisDelayedVestingAccount creates a new DelayedVestingAccount with the
// specified parameters. It returns the created account converted to genesis
// account type and the account balance.
// The final vesting balance is calculated by subtracting the initial unlocked coins
// from the vesting balance.
func NewGenesisDelayedVestingAccount(
	address string,
	vestingBalance,
	initUnlockedCoins sdk.Coins,
	endTime time.Time,
) (account authtypes.GenesisAccount, balance banktypes.Balance, err error) {
	sdkAddr, err := sdk.AccAddressFromBech32(address)
	if err != nil {
		return account, balance, err
	}

	balance = banktypes.Balance{
		Address: address,
		Coins:   vestingBalance,
	}
	vestingBalance = vestingBalance.Sub(initUnlockedCoins...)

	bAccount := authtypes.NewBaseAccountWithAddress(sdkAddr)
	vAccount := vestingtypes.NewDelayedVestingAccount(bAccount, vestingBalance, endTime.Unix())

	return authtypes.GenesisAccount(vAccount), balance, nil
}

// The final vesting balance is calculated by subtracting the initial unlocked coins
// from the vesting balance.
func NewGenesisPeriodicVestingAccount(
	address string,
	vestingBalance,
	initUnlockedCoins sdk.Coins,
	startTime time.Time,
	periods []vestingtypes.Period,
) (account authtypes.GenesisAccount, balance banktypes.Balance, err error) {
	sdkAddr, err := sdk.AccAddressFromBech32(address)
	if err != nil {
		return account, balance, err
	}

	balance = banktypes.Balance{
		Address: address,
		Coins:   vestingBalance,
	}
	vestingBalance = vestingBalance.Sub(initUnlockedCoins...)

	bAccount := authtypes.NewBaseAccountWithAddress(sdkAddr)
	vAccount := vestingtypes.NewPeriodicVestingAccount(bAccount, vestingBalance, startTime.Unix(), periods)

	return authtypes.GenesisAccount(vAccount), balance, nil
}

// The final vesting balance is calculated by subtracting the initial unlocked coins
// from the vesting balance.
func NewGenesisContinuousVestingAccount(
	address string,
	vestingBalance,
	initUnlockedCoins sdk.Coins,
	startTime, endTime time.Time,
) (account authtypes.GenesisAccount, balance banktypes.Balance, err error) {
	sdkAddr, err := sdk.AccAddressFromBech32(address)
	if err != nil {
		return account, balance, err
	}

	balance = banktypes.Balance{
		Address: address,
		Coins:   vestingBalance,
	}
	vestingBalance = vestingBalance.Sub(initUnlockedCoins...)

	bAccount := authtypes.NewBaseAccountWithAddress(sdkAddr)
	vAccount := vestingtypes.NewContinuousVestingAccount(bAccount, vestingBalance, startTime.Unix(), endTime.Unix())

	return authtypes.GenesisAccount(vAccount), balance, nil
}

// AddAccountsToGenesisState adds the provided accounts to the genesis state (gs) map for the auth module.
// It takes the raw genesis state (gs) and a variadic number of GenesisAccount objects (accounts) as inputs.
// Then, it updates the given genesis state and returns it.
func AddAccountsToGenesisState(encCfg encoding.Config, gs map[string]json.RawMessage, accounts ...authtypes.GenesisAccount) (map[string]json.RawMessage, error) {
	var authGenState authtypes.GenesisState
	err := encCfg.Codec.UnmarshalJSON(gs[authtypes.ModuleName], &authGenState)
	if err != nil {
		return gs, err
	}

	pAccs, err := authtypes.PackAccounts(accounts)
	if err != nil {
		return gs, err
	}

	// set the accounts in the genesis state
	authGenState.Accounts = append(authGenState.Accounts, pAccs...)
	gs[authtypes.ModuleName] = encCfg.Codec.MustMarshalJSON(&authGenState)

	return gs, nil
}

// AddBalancesToGenesisState updates the genesis state by adding balances to the bank module.
func AddBalancesToGenesisState(encCfg encoding.Config, gs map[string]json.RawMessage, balances []banktypes.Balance) (map[string]json.RawMessage, error) {
	var bankGenState banktypes.GenesisState
	err := encCfg.Codec.UnmarshalJSON(gs[banktypes.ModuleName], &bankGenState)
	if err != nil {
		return gs, err
	}

	bankGenState.Balances = append(bankGenState.Balances, balances...)
	gs[banktypes.ModuleName] = encCfg.Codec.MustMarshalJSON(&bankGenState)

	return gs, nil
}

// AddGenesisAccountsWithBalancesToGenesisState adds the given genesis accounts and balances to the
// provided genesis state. It returns the updated genesis state and an error if any occurred.
func AddGenesisAccountsWithBalancesToGenesisState(
	encCfg encoding.Config,
	gs map[string]json.RawMessage,
	gAccounts []authtypes.GenesisAccount,
	balances []banktypes.Balance,
) (map[string]json.RawMessage, error) {
	gs, err := AddAccountsToGenesisState(encCfg, gs, gAccounts...)
	if err != nil {
		return gs, err
	}

	gs, err = AddBalancesToGenesisState(encCfg, gs, balances)
	if err != nil {
		return gs, err
	}
	return gs, nil
}

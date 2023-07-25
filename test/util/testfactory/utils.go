package testfactory

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"time"

	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	rpctypes "github.com/tendermint/tendermint/rpc/core/types"
	tmtime "github.com/tendermint/tendermint/types/time"
	"google.golang.org/grpc"
)

const (
	// nolint:lll
	TestAccName               = "test-account"
	TestAccMnemo              = `ramp soldier connect gadget domain mutual staff unusual first midnight iron good deputy wage vehicle mutual spike unlock rocket delay hundred script tumble choose`
	bondDenom                 = "utia"
	BaseAccountDefaultBalance = 10000
)

func QueryWithoutProof(clientCtx client.Context, hashHexStr string) (*rpctypes.ResultTx, error) {
	hash, err := hex.DecodeString(hashHexStr)
	if err != nil {
		return nil, err
	}

	node, err := clientCtx.GetNode()
	if err != nil {
		return nil, err
	}

	return node.Tx(context.Background(), hash, false)
}

func GenerateKeyring(accounts ...string) keyring.Keyring {
	cdc := simapp.MakeTestEncodingConfig().Codec
	kb := keyring.NewInMemory(cdc)

	for _, acc := range accounts {
		_, _, err := kb.NewMnemonic(acc, keyring.English, "", "", hd.Secp256k1)
		if err != nil {
			panic(err)
		}
	}

	_, err := kb.NewAccount(TestAccName, TestAccMnemo, "", "", hd.Secp256k1)
	if err != nil {
		panic(err)
	}

	return kb
}

func RandomAddress() sdk.Address {
	name := tmrand.Str(6)
	kr := GenerateKeyring(name)
	rec, err := kr.Key(name)
	if err != nil {
		panic(err)
	}
	addr, err := rec.GetAddress()
	if err != nil {
		panic(err)
	}
	return addr
}

func FundKeyringAccounts(accounts ...string) (keyring.Keyring, []banktypes.Balance, []authtypes.GenesisAccount) {
	kr := GenerateKeyring(accounts...)
	genAccounts := make([]authtypes.GenesisAccount, len(accounts))
	genBalances := make([]banktypes.Balance, len(accounts))

	for i, acc := range accounts {
		rec, err := kr.Key(acc)
		if err != nil {
			panic(err)
		}

		addr, err := rec.GetAddress()
		if err != nil {
			panic(err)
		}

		balances := sdk.NewCoins(
			sdk.NewCoin(bondDenom, sdk.NewInt(99999999999999999)),
		)

		genBalances[i] = banktypes.Balance{Address: addr.String(), Coins: balances.Sort()}
		genAccounts[i] = authtypes.NewBaseAccount(addr, nil, uint64(i), 0)
	}
	return kr, genBalances, genAccounts
}

func GenerateAccounts(count int) []string {
	accs := make([]string, count)
	for i := 0; i < count; i++ {
		accs[i] = tmrand.Str(20)
	}
	return accs
}

func NewBaseAccount(kr keyring.Keyring, name string) (*authtypes.BaseAccount, sdk.Coins) {
	if name == "" {
		name = tmrand.Str(6)
	}
	rec, _, err := kr.NewMnemonic(name, keyring.English, "", "", hd.Secp256k1)
	if err != nil {
		panic(err)
	}
	addr, err := rec.GetAddress()
	if err != nil {
		panic(err)
	}
	origCoins := sdk.Coins{sdk.NewInt64Coin(bondDenom, BaseAccountDefaultBalance)}
	bacc := authtypes.NewBaseAccountWithAddress(addr)
	return bacc, origCoins
}

// GenerateBaseAccounts generates base accounts and their corresponding balances.
// It takes a keyring.Keyring instance and a slice of account names as inputs.
// For each name in the names slice, it creates a new base account and adds it to
// the bAccounts slice. It also creates a banktypes.Balance struct for the account,
// with the account's address and coins including an initial coin for gas fee.
// The balances are added to the balances slice.
//
// Parameters:
// - kr: A keyring.Keyring instance used to create base accounts.
// - names: A slice of account names.
// - initExtraCoins: A slice of sdk.Coins representing the extra coins to be added to
// the base accounts which can be used for example for gas fees.
//
// Returns:
// - bAccounts: A slice of authtypes.BaseAccount representing the generated base accounts.
// - balances: A slice of banktypes.Balance representing the balances of the generated accounts.
func GenerateBaseAccounts(kr keyring.Keyring, names []string, initExtraCoins ...sdk.Coin) ([]authtypes.BaseAccount, []banktypes.Balance) {
	bAccounts := []authtypes.BaseAccount{}
	balances := []banktypes.Balance{}
	for _, name := range names {
		acc, coins := NewBaseAccount(kr, name)
		bAccounts = append(bAccounts, *acc)
		balances = append(balances, banktypes.Balance{
			Address: acc.GetAddress().String(),
			Coins:   coins.Add(initExtraCoins...),
		})
	}

	return bAccounts, balances
}

// GenerateDelayedVestingAccounts generates delayed vesting accounts.
//
// Parameters:
// - kr: The keyring.
// - names: The names of the accounts.
// - initUnlockedCoins: Optional initial unlocked coins for each account (e.g. for gas fees).
//
// Returns:
// - []*vestingtypes.DelayedVestingAccount: The generated delayed vesting accounts.
// - []banktypes.Balance: The balances of the accounts.
func GenerateDelayedVestingAccounts(kr keyring.Keyring, names []string, initUnlockedCoins ...sdk.Coin) ([]*vestingtypes.DelayedVestingAccount, []banktypes.Balance) {
	bAccounts, balances := GenerateBaseAccounts(kr, names, initUnlockedCoins...)
	vAccounts := []*vestingtypes.DelayedVestingAccount{}

	endTime := tmtime.Now().Add(-2 * time.Second)
	for i := range bAccounts {
		bal := FindBalanceByAddress(balances, bAccounts[i].GetAddress().String())
		coins := bal.Coins.Sub(initUnlockedCoins...)
		acc := vestingtypes.NewDelayedVestingAccount(&bAccounts[i], coins, endTime.Unix())
		vAccounts = append(vAccounts, acc)

		// the endTime is increased for each account to be able to test various scenarios
		endTime = endTime.Add(5 * time.Second)
	}

	return vAccounts, balances
}

// GeneratePeriodicVestingAccounts generates periodic vesting accounts.
//
// Parameters:
// - kr: The keyring used to generate the accounts.
// - names: The names of the accounts to be generated.
// - initUnlockedCoins: Optional initial unlocked coins for each account (e.g. for gas fees).
//
// Returns:
// - vAccounts: The generated periodic vesting accounts.
// - balances: The balances of the generated accounts.
func GeneratePeriodicVestingAccounts(kr keyring.Keyring, names []string, initUnlockedCoins ...sdk.Coin) ([]*vestingtypes.PeriodicVestingAccount, []banktypes.Balance) {
	bAccounts, balances := GenerateBaseAccounts(kr, names, initUnlockedCoins...)
	vAccounts := []*vestingtypes.PeriodicVestingAccount{}

	vestingAmount := balances[0].Coins.AmountOf(bondDenom).Int64()
	allocationPerPeriod := vestingAmount / 4
	periods := vestingtypes.Periods{
		vestingtypes.Period{Length: int64(6), Amount: sdk.Coins{sdk.NewInt64Coin(bondDenom, allocationPerPeriod)}},
		vestingtypes.Period{Length: int64(6), Amount: sdk.Coins{sdk.NewInt64Coin(bondDenom, allocationPerPeriod)}},
		vestingtypes.Period{Length: int64(6), Amount: sdk.Coins{sdk.NewInt64Coin(bondDenom, allocationPerPeriod)}},
		vestingtypes.Period{Length: int64(6), Amount: sdk.Coins{sdk.NewInt64Coin(bondDenom, allocationPerPeriod)}},
	}

	startTime := tmtime.Now()
	for i := range bAccounts {
		bal := FindBalanceByAddress(balances, bAccounts[i].GetAddress().String())
		coins := bal.Coins.Sub(initUnlockedCoins...)
		acc := vestingtypes.NewPeriodicVestingAccount(&bAccounts[i], coins, startTime.Unix(), periods)
		vAccounts = append(vAccounts, acc)

		// the startTime is increased for each account to be able to test various scenarios
		startTime = startTime.Add(5 * time.Second)
	}

	return vAccounts, balances
}

// GenerateContinuousVestingAccounts generates continuous vesting accounts.
//
// Parameters:
// - kr: keyring.Keyring
// - names: []string
// - initUnlockedCoins: Optional initial unlocked coins for each account (e.g. for gas fees).
//
// Returns:
// - vAccounts: The generated continuous vesting accounts.
// - balances: The balances of the generated accounts.
func GenerateContinuousVestingAccounts(kr keyring.Keyring, names []string, initUnlockedCoins ...sdk.Coin) ([]*vestingtypes.ContinuousVestingAccount, []banktypes.Balance) {
	bAccounts, balances := GenerateBaseAccounts(kr, names, initUnlockedCoins...)
	vAccounts := []*vestingtypes.ContinuousVestingAccount{}

	startTime := tmtime.Now()

	for i := range bAccounts {
		bal := FindBalanceByAddress(balances, bAccounts[i].GetAddress().String())
		coins := bal.Coins.Sub(initUnlockedCoins...)

		endTime := startTime.Add(10 * time.Second)
		acc := vestingtypes.NewContinuousVestingAccount(&bAccounts[i], coins, startTime.Unix(), endTime.Unix())
		vAccounts = append(vAccounts, acc)

		// the startTime is increased for each account to be able to test various scenarios
		startTime = startTime.Add(5 * time.Second)
	}

	return vAccounts, balances
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

// AddBalancesToGenesisState adds the provided balances to the genesis state (gs) for the bank module.
// It takes the raw genesis state (gs) and a variadic number of Balance objects (balances) as inputs.
// It returns the updated gs and nil if the process is successful.
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

// GetValidators retrieves the validators from the staking module using the provided gRPC client connection (grpcConn).
// It takes a gRPC client connection (grpcConn) as input.
// Then, it returns the validators from the response.
func GetValidators(grpcConn *grpc.ClientConn) (stakingtypes.Validators, error) {
	scli := stakingtypes.NewQueryClient(grpcConn)
	vres, err := scli.Validators(context.Background(), &stakingtypes.QueryValidatorsRequest{})

	if vres == nil {
		return stakingtypes.Validators{}, err
	}
	return vres.Validators, err
}

// GetAccountDelegations retrieves the delegations for the specified account address using the provided gRPC client connection (grpcConn).
// It takes a gRPC client connection (grpcConn) and the account address (address) as inputs.
// If an error occurs during the request, it returns nil and the error.
// Otherwise, it returns the delegation responses from the response.
func GetAccountDelegations(grpcConn *grpc.ClientConn, address string) (stakingtypes.DelegationResponses, error) {
	cli := stakingtypes.NewQueryClient(grpcConn)
	res, err := cli.DelegatorDelegations(context.Background(),
		&stakingtypes.QueryDelegatorDelegationsRequest{DelegatorAddr: address})
	if err != nil {
		return nil, err
	}

	return res.DelegationResponses, err
}

// GetAccountSpendableBalance retrieves the spendable balance of an account for the specified address using gRPC.
// It takes a gRPC client connection (grpcConn) and the account address (address) as inputs.
// If the account is not found or an error occurs, it returns nil and the error.
// Otherwise, it returns the spendable balances of the account as an sdk.Coins object.
//
// Parameters:
// - grpcConn: A gRPC client connection.
// - address: The account address to retrieve the spendable balances for.
//
// Returns:
// The spendable balances of the account as an sdk.Coins object, query time, or nil and an error if the retrieval fails.
func GetAccountSpendableBalance(grpcConn *grpc.ClientConn, address string) (balances sdk.Coins, queryTime int64, err error) {
	cli := banktypes.NewQueryClient(grpcConn)
	res, err := cli.SpendableBalances(
		context.Background(),
		&banktypes.QuerySpendableBalancesRequest{
			Address: address,
		},
	)
	if err != nil || res == nil {
		return nil, 0, err
	}
	return res.GetBalances(), res.QueryTime, nil
}

// GetRawAccountInfo retrieves the raw account information for the specified address using gRPC.
// It takes a gRPC client connection (grpcConn) and the account address (address) as inputs.
// If no account found or an error occurs, it returns nil and the error.
// Otherwise, it returns the value field of the account in the response, which represents the raw account information.
//
// Parameters:
// - grpcConn: A gRPC client connection.
// - address: The account address to retrieve the raw information for.
//
// Returns:
// The raw account information as a byte slice, or nil and an error if the retrieval fails.
func GetRawAccountInfo(grpcConn *grpc.ClientConn, address string) ([]byte, error) {
	cli := authtypes.NewQueryClient(grpcConn)
	res, err := cli.Account(context.Background(), &authtypes.QueryAccountRequest{
		Address: address,
	})

	if err != nil || res == nil {
		return nil, err
	}

	return res.Account.Value, nil
}

// FindBalanceByAddress finds a balance in the given slice of banktypes.Balance
// by matching the address with the provided address string.
// It iterates over the balances slice and returns the pointer to the first balance
// that has a matching address. If no matching balance is found, it returns nil.
//
// Parameters:
// - balances: A slice of banktypes.Balance to search within.
// - address: The address string to match against balance addresses.
//
// Returns:
// A pointer to the banktypes.Balance with a matching address, or nil if not found.
func FindBalanceByAddress(balances []banktypes.Balance, address string) *banktypes.Balance {
	for _, b := range balances {
		if b.Address == address {
			return &b
		}
	}
	return nil
}

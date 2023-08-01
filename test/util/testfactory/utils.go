package testfactory

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	grpctypes "github.com/cosmos/cosmos-sdk/types/grpc"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	vestingtypes "github.com/cosmos/cosmos-sdk/x/auth/vesting/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	rpctypes "github.com/tendermint/tendermint/rpc/core/types"
	coretypes "github.com/tendermint/tendermint/types"
	tmtime "github.com/tendermint/tendermint/types/time"
	"google.golang.org/grpc"
	"google.golang.org/grpc/metadata"
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

func GenerateBaseAccounts(kr keyring.Keyring, names []string, initExtraCoins ...sdk.Coin) ([]authtypes.BaseAccount, []banktypes.Balance) {
	bAccounts := make([]authtypes.BaseAccount, len(names))
	balances := make([]banktypes.Balance, len(names))
	for i, name := range names {
		acc, coins := NewBaseAccount(kr, name)
		bAccounts[i] = *acc
		balances[i] = banktypes.Balance{
			Address: acc.GetAddress().String(),
			Coins:   coins.Add(initExtraCoins...),
		}
	}

	return bAccounts, balances
}

func GenerateDelayedVestingAccounts(kr keyring.Keyring, names []string, initUnlockedCoins ...sdk.Coin) ([]*vestingtypes.DelayedVestingAccount, []banktypes.Balance) {
	bAccounts, balances := GenerateBaseAccounts(kr, names, initUnlockedCoins...)
	vAccounts := []*vestingtypes.DelayedVestingAccount{}

	endTime := tmtime.Now().Add(-2 * time.Second)
	for i := range bAccounts {
		bal := balances[i]
		// initUnlockedCoins are subbed to keep them unlocked
		coins := bal.Coins.Sub(initUnlockedCoins...)
		acc := vestingtypes.NewDelayedVestingAccount(&bAccounts[i], coins, endTime.Unix())
		vAccounts = append(vAccounts, acc)

		// the endTime is increased for each account to be able to test various scenarios
		endTime = endTime.Add(5 * time.Second)
	}

	return vAccounts, balances
}

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
		bal := balances[i]
		// initUnlockedCoins are subbed to keep them unlocked
		coins := bal.Coins.Sub(initUnlockedCoins...)
		acc := vestingtypes.NewPeriodicVestingAccount(&bAccounts[i], coins, startTime.Unix(), periods)
		vAccounts = append(vAccounts, acc)

		// the startTime is increased for each account to be able to test various scenarios
		startTime = startTime.Add(5 * time.Second)
	}

	return vAccounts, balances
}

func GenerateContinuousVestingAccounts(kr keyring.Keyring, names []string, initUnlockedCoins ...sdk.Coin) ([]*vestingtypes.ContinuousVestingAccount, []banktypes.Balance) {
	bAccounts, balances := GenerateBaseAccounts(kr, names, initUnlockedCoins...)
	vAccounts := []*vestingtypes.ContinuousVestingAccount{}

	startTime := tmtime.Now()

	for i := range bAccounts {
		bal := balances[i]
		// initUnlockedCoins are subbed to keep them unlocked
		coins := bal.Coins.Sub(initUnlockedCoins...)

		endTime := startTime.Add(20 * time.Second)
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

func GetValidators(grpcConn *grpc.ClientConn) (stakingtypes.Validators, error) {
	scli := stakingtypes.NewQueryClient(grpcConn)
	vres, err := scli.Validators(context.Background(), &stakingtypes.QueryValidatorsRequest{})

	if vres == nil {
		return stakingtypes.Validators{}, err
	}
	return vres.Validators, err
}

func GetAccountDelegations(grpcConn *grpc.ClientConn, address string) (stakingtypes.DelegationResponses, error) {
	cli := stakingtypes.NewQueryClient(grpcConn)
	res, err := cli.DelegatorDelegations(context.Background(),
		&stakingtypes.QueryDelegatorDelegationsRequest{DelegatorAddr: address})
	if err != nil {
		return nil, err
	}

	return res.DelegationResponses, err
}

func GetAccountSpendableBalance(grpcConn *grpc.ClientConn, address string) (balances sdk.Coins, err error) {
	cli := banktypes.NewQueryClient(grpcConn)
	res, err := cli.SpendableBalances(
		context.Background(),
		&banktypes.QuerySpendableBalancesRequest{
			Address: address,
		},
	)
	if err != nil || res == nil {
		return nil, err
	}
	return res.GetBalances(), nil
}

// GetAccountSpendableBalanceByBlock retrieves the spendable balance of an account for the specified address at the given block using gRPC.
func GetAccountSpendableBalanceByBlock(grpcConn *grpc.ClientConn, address string, block *coretypes.Block) (balances sdk.Coins, err error) {
	cli := banktypes.NewQueryClient(grpcConn)
	ctx := metadata.AppendToOutgoingContext(context.Background(), grpctypes.GRPCBlockHeightHeader, fmt.Sprint(block.Height))

	// Since the blockTime is not inferred from block height to the GRPC server, we use the following
	// line; however, it is commented out because we do not want to modify the SDK code to support it
	// ctx = metadata.AppendToOutgoingContext(ctx, grpctypes.GRPCBlockTimeHeader, fmt.Sprint(block.Time.Unix()))

	res, err := cli.SpendableBalances(
		ctx,
		&banktypes.QuerySpendableBalancesRequest{
			Address: address,
		},
	)
	if err != nil || res == nil {
		return nil, err
	}
	return res.GetBalances(), nil
}

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

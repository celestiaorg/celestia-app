package testfactory

import (
	"context"
	"encoding/hex"

	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/simapp"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	rpctypes "github.com/tendermint/tendermint/rpc/core/types"
)

const (
	// nolint:lll
	TestAccName  = "test-account"
	TestAccMnemo = `ramp soldier connect gadget domain mutual staff unusual first midnight iron good deputy wage vehicle mutual spike unlock rocket delay hundred script tumble choose`
	bondDenom    = "utia"
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
		_, _, err := kb.NewMnemonic(acc, keyring.English, keyring.DefaultBIP39Passphrase, sdk.FullFundraiserPath, hd.Secp256k1)
		if err != nil {
			panic(err)
		}
	}

	_, err := kb.NewAccount(TestAccName, TestAccMnemo, keyring.DefaultBIP39Passphrase, sdk.FullFundraiserPath, hd.Secp256k1)
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
	genBalances, genAccounts := FundAllKeyringAccounts(kr, 99999999999999999)
	return kr, genBalances, genAccounts
}

func FundAllKeyringAccounts(keys keyring.Keyring, amount int64) ([]banktypes.Balance, []authtypes.GenesisAccount) {
	records, err := keys.List()
	if err != nil {
		panic(err)
	}

	genAccounts := make([]authtypes.GenesisAccount, len(records))
	genBalances := make([]banktypes.Balance, len(records))

	for i, rec := range records {
		addr, err := rec.GetAddress()
		if err != nil {
			panic(err)
		}

		balances := sdk.NewCoins(
			sdk.NewCoin(bondDenom, sdk.NewInt(amount)),
		)

		genBalances[i] = banktypes.Balance{Address: addr.String(), Coins: balances.Sort()}
		genAccounts[i] = authtypes.NewBaseAccount(addr, nil, uint64(i), 0)
	}
	return genBalances, genAccounts
}

func GenerateAccounts(count int) []string {
	accs := make([]string, count)
	for i := 0; i < count; i++ {
		accs[i] = tmrand.Str(20)
	}
	return accs
}

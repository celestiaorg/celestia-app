package spoon

import (
	"encoding/json"
	"fmt"
	"io/ioutil"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	disttypes "github.com/cosmos/cosmos-sdk/x/distribution/types"
	"github.com/spf13/cobra"
	coretypes "github.com/tendermint/tendermint/types"
)

var encCfg encoding.Config

func init() {
	encCfg = encoding.MakeConfig(app.ModuleEncodingRegisters...)
}

func CmdSoftSpoon() *cobra.Command {
	command := &cobra.Command{
		Use:   "hardspoon [pathToExportedGenesis] [pathToNewGenesis]",
		Short: "hardspoon an exported chain by copying the balances from one state to a new one",
		Args:  cobra.ExactArgs(2),
		RunE: func(cmd *cobra.Command, args []string) error {
			srcPath := args[0]
			dstPath := args[1]
			_, _, err := Fork(srcPath, dstPath)

			return err
		},
	}

	return command
}

func Fork(src, dst string, newAccounts ...string) (map[string]json.RawMessage, keyring.Keyring, error) {
	oldstate, err := loadGenState(src)
	if err != nil {
		return nil, nil, err
	}

	newstate, err := loadGenState(dst)
	if err != nil {
		return nil, nil, err
	}

	newstate, kr := moveAccounts(oldstate, newstate, newAccounts...)

	gbz, err := json.MarshalIndent(newstate, "", "    ")
	if err != nil {
		return nil, nil, err
	}

	gdoc := coretypes.GenesisDoc{
		GenesisTime:     time.Now(),
		ChainID:         "mocha-1",
		InitialHeight:   0,
		ConsensusParams: coretypes.DefaultConsensusParams(),
		AppState:        gbz,
	}

	return newstate, kr, gdoc.SaveAs(dst)
}

func loadGenState(path string) (map[string]json.RawMessage, error) {
	// load the genesis script
	jsonBlob, err := ioutil.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("couldn't load genesis state %w", err)
	}

	rawappstate := peel(jsonBlob, "app_state")
	var appstate map[string]json.RawMessage
	err = json.Unmarshal(rawappstate, &appstate)
	if err != nil {
		panic(err)
	}

	return appstate, nil
}

// accounts is added here for testing purposes only, do not list accounts here
// or more funds will be given to that account.
func moveAccounts(src, dst map[string]json.RawMessage, accounts ...string) (map[string]json.RawMessage, keyring.Keyring) {
	// unmarshal the accounts of the src state
	srcBankState, srcAuthState, srcDistState := unmarshalAccountState(src)
	dstBankState, dstAuthState, _ := unmarshalAccountState(dst)

	// return all staked funds back to users
	srcBankState.Balances = returnStakedFunds(srcDistState, srcBankState.Balances)

	// remove the balances for module accounts
	prunedBankBals, prunedAuthAccs := removeModuleAccounts(srcBankState.Balances, srcAuthState.Accounts)
	srcBankState.Balances, srcAuthState.Accounts = prunedBankBals, prunedAuthAccs

	// for testing purposes only, do not put new accounts in IRL
	kr, b, a := fundKeyringAccounts(encCfg.Codec, accounts...)

	// copy over the accounts
	dstBankState.Balances = append(dstBankState.Balances, srcBankState.Balances...)
	dstBankState.Balances = append(dstBankState.Balances, b...)

	totalSupply := sdk.NewCoins()
	for _, newAcc := range dstBankState.Balances {
		totalSupply = totalSupply.Add(newAcc.Coins...)
	}
	fmt.Println("total supply", totalSupply)
	dstBankState.Supply = totalSupply
	dstAuthState.Accounts = append(dstAuthState.Accounts, srcAuthState.Accounts...)
	dstAuthState.Accounts = append(dstAuthState.Accounts, a...)

	dst[banktypes.ModuleName] = encCfg.Codec.MustMarshalJSON(&dstBankState)
	dst[authtypes.ModuleName] = encCfg.Codec.MustMarshalJSON(&dstAuthState)

	return dst, kr
}

func returnStakedFunds(d disttypes.GenesisState, existing []banktypes.Balance) []banktypes.Balance {
	balMap := make(map[string]banktypes.Balance)
	for _, b := range existing {
		_, has := balMap[b.Address]
		if has {
			panic("duplicate address")
		}
		balMap[b.Address] = b
	}
	for _, infos := range d.DelegatorStartingInfos {
		truncCoins, _ := sdk.NewDecCoins(
			sdk.NewDecCoinFromDec(
				app.BondDenom,
				infos.StartingInfo.Stake),
		).TruncateDecimal()
		balance, ok := balMap[infos.DelegatorAddress]
		if !ok {
			balance = banktypes.Balance{Address: infos.DelegatorAddress, Coins: truncCoins}
		} else {
			balance.Coins = balance.Coins.Add(truncCoins...)
		}
		balMap[infos.DelegatorAddress] = balance
	}
	bals := make([]banktypes.Balance, 0, len(balMap))
	for _, b := range balMap {
		bals = append(bals, b)
	}
	return bals
}

func removeModuleAccounts(bals []banktypes.Balance, accs []*codectypes.Any) ([]banktypes.Balance, []*codectypes.Any) {
	uaccs, err := authtypes.UnpackAccounts(accs)
	if err != nil {
		panic(err)
	}

	balMap := make(map[string]banktypes.Balance)
	for _, bal := range bals {
		balMap[bal.Address] = bal
	}

	for i, acc := range uaccs {
		macc, ok := acc.(*authtypes.ModuleAccount)
		if !ok {
			continue
		}

		delete(balMap, macc.Address)
		copy(uaccs[i:], uaccs[i+1:])
		uaccs[len(uaccs)-1] = nil
		uaccs = uaccs[:len(uaccs)-1]
	}

	bals = make([]banktypes.Balance, 0, len(balMap))
	for _, v := range balMap {
		bals = append(bals, v)
	}

	return bals, accs
}

func fundKeyringAccounts(cdc codec.Codec, accounts ...string) (keyring.Keyring, []banktypes.Balance, []*codectypes.Any) {
	kb := keyring.NewInMemory(cdc)
	genAccounts := make([]authtypes.GenesisAccount, len(accounts))
	genBalances := make([]banktypes.Balance, len(accounts))

	for i, acc := range accounts {
		rec, _, err := kb.NewMnemonic(acc, keyring.English, "", "", hd.Secp256k1)
		if err != nil {
			panic(err)
		}

		addr, err := rec.GetAddress()
		if err != nil {
			panic(err)
		}

		balances := sdk.NewCoins(
			sdk.NewCoin(app.BondDenom, sdk.NewInt(1000000000000)),
		)

		genBalances[i] = banktypes.Balance{Address: addr.String(), Coins: balances.Sort()}
		genAccounts[i] = authtypes.NewBaseAccount(addr, nil, 0, 0)
	}

	authAccs, err := authtypes.PackAccounts(genAccounts)
	if err != nil {
		panic(err)
	}

	return kb, genBalances, authAccs
}

func unmarshalAccountState(genState map[string]json.RawMessage) (banktypes.GenesisState, authtypes.GenesisState, disttypes.GenesisState) {
	var bankGenState banktypes.GenesisState
	encCfg.Codec.MustUnmarshalJSON(genState[banktypes.ModuleName], &bankGenState)

	var authGenState authtypes.GenesisState
	encCfg.Codec.MustUnmarshalJSON(genState[authtypes.ModuleName], &authGenState)

	var distGenState disttypes.GenesisState
	encCfg.Codec.MustUnmarshalJSON(genState[disttypes.ModuleName], &distGenState)
	return bankGenState, authGenState, distGenState
}

func printKeys(b json.RawMessage) {
	var m map[string]json.RawMessage
	err := json.Unmarshal(b, &m)
	if err != nil {
		panic(err)
	}
	for k := range m {
		fmt.Println(k)
	}
}

func peel(n json.RawMessage, key string) json.RawMessage {
	var m map[string]json.RawMessage
	err := json.Unmarshal(n, &m)
	if err != nil {
		panic(err)
	}
	return m[key]
}

func seel(state, module map[string]json.RawMessage, key string) map[string]json.RawMessage {
	moduleBz, err := json.Marshal(module)
	if err != nil {
		panic(err)
	}
	state[key] = moduleBz
	return state
}

func defaultGenState() map[string]json.RawMessage {
	encCfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	return app.ModuleBasics.DefaultGenesis(encCfg.Codec)
}

func copyModuleState(src, dst map[string]json.RawMessage, modules ...string) map[string]json.RawMessage {
	for _, m := range modules {
		dst[m] = src[m]
	}
	return dst
}

package testground

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// Genesis manages the creation of the genesis state of a network. It is meant
// to be used as the first step to any test that requires a network.
type Genesis struct {
	ecfg encoding.Config

	// kr is the keyring used to generate the genesis accounts and validators.
	// Transaction keys for all genesis accounts are stored in this keyring and
	// are indexed by account name. Public keys and addresses can be derived
	// from those keys using the existing keyring API.
	kr keyring.Keyring

	// ChainID is the chain ID of the network.
	ChainID string
	// GenesisTime is the genesis time of the network.
	GenesisTime time.Time

	// accounts are the genesis accounts that will be included in the genesis.
	accounts []GenesisAccount
	// validators are the validators of the network. Note that each validator
	// also has a genesis account.
	validators []Validator
	// genTxs are the genesis transactions that will be included in the genesis.
	// Transactions are generated upon adding a validator to the genesis.
	genTxs []sdk.Tx

	// ConsensusParams are the consensus parameters of the network.
	ConsensusParams *tmproto.ConsensusParams
}

func (g *Genesis) AddGenesisAccount(acc GenesisAccount) error {
	if err := acc.ValidateBasic(); err != nil {
		return err
	}
	_, _, err := g.kr.NewMnemonic(acc.Name, keyring.English, "", "", hd.Secp256k1)
	if err != nil {
		return err
	}
	g.accounts = append(g.accounts, acc)
	return nil
}

func (g *Genesis) AddValidator(val Validator) error {
	if err := val.ValidateBasic(); err != nil {
		return err
	}

	// Add the validator's genesis account
	if err := g.AddGenesisAccount(val.GenesisAccount); err != nil {
		return err
	}

	// Add the validator's genesis transaction
	gentx, err := val.GenTx(g.ecfg, g.kr, g.ChainID)
	if err != nil {
		return err
	}

	// install the validator
	g.genTxs = append(g.genTxs, gentx)
	g.validators = append(g.validators, val)
	return nil
}

// GenesisAccounts returns the genesis accounts of the network. This includes
// the genesis accounts in the validators.
func (g *Genesis) GenesisAccounts() []GenesisAccount {
	accs := make([]GenesisAccount, len(g.accounts)+len(g.validators))
	for i, acc := range g.accounts {
		accs[i] = acc
	}
	for i, val := range g.validators {
		accs[i+len(g.accounts)] = val.GenesisAccount
	}
	return accs
}

// genAccountsToSDKTypes converts the genesis accounts to native SDK types.
func genAccountsToSDKTypes(kr keyring.Keyring, accs []GenesisAccount) ([]banktypes.Balance, []authtypes.GenesisAccount, error) {
	genBals := make([]banktypes.Balance, len(accs))
	genAccs := make([]authtypes.GenesisAccount, len(accs))
	for i, acc := range accs {
		rec, err := kr.Key(acc.Name)
		if err != nil {
			return nil, nil, err
		}
		addr, err := rec.GetAddress()
		if err != nil {
			return nil, nil, err
		}
		pubKey, err := rec.GetPubKey()
		if err != nil {
			return nil, nil, err
		}
		balances := sdk.NewCoins(
			sdk.NewCoin(appconsts.BondDenom, sdk.NewInt(acc.InitialTokens)),
		)
		genBals[i] = banktypes.Balance{Address: addr.String(), Coins: balances.Sort()}
		genAccs[i] = authtypes.NewBaseAccount(addr, pubKey, uint64(i), 0)
	}
	return genBals, genAccs, nil
}

func (g *Genesis) Export() (*coretypes.GenesisDoc, error) {
	authGenState := authtypes.DefaultGenesisState()
	bankGenState := banktypes.DefaultGenesisState()
	genutilGenState := genutiltypes.DefaultGenesisState()

	genBals, genAccs, err := genAccountsToSDKTypes(g.kr, g.GenesisAccounts())
	if err != nil {
		return nil, err
	}

	accounts, err := authtypes.PackAccounts(genAccs)
	if err != nil {
		return nil, err
	}
	authGenState.Accounts = append(authGenState.Accounts, accounts...)
	bankGenState.Balances = append(bankGenState.Balances, genBals...)

	for _, genTx := range g.genTxs {
		bz, err := g.ecfg.TxConfig.TxJSONEncoder()(genTx)
		if err != nil {
			return nil, err
		}

		genutilGenState.GenTxs = append(genutilGenState.GenTxs, json.RawMessage(bz))
	}

	// perform some basic validation of the genesis state
	if err := authtypes.ValidateGenesis(*authGenState); err != nil {
		return nil, err
	}
	if err := bankGenState.Validate(); err != nil {
		return nil, err
	}
	if err := genutiltypes.ValidateGenesis(genutilGenState, g.ecfg.TxConfig.TxJSONDecoder()); err != nil {
		return nil, err
	}

	state := app.ModuleBasics.DefaultGenesis(g.ecfg.Codec)
	state[authtypes.ModuleName] = g.ecfg.Codec.MustMarshalJSON(authGenState)
	state[banktypes.ModuleName] = g.ecfg.Codec.MustMarshalJSON(bankGenState)
	state[genutiltypes.ModuleName] = g.ecfg.Codec.MustMarshalJSON(genutilGenState)

	stateBz, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return nil, err
	}

	// Create the genesis doc
	genesisDoc := &coretypes.GenesisDoc{
		ChainID:         g.ChainID,
		GenesisTime:     g.GenesisTime,
		ConsensusParams: g.ConsensusParams,
		AppState:        stateBz,
	}

	return genesisDoc, nil
}

func (g *Genesis) Keyring() keyring.Keyring {
	return g.kr
}

type GenesisAccount struct {
	Name          string
	InitialTokens int64
}

func (ga *GenesisAccount) ValidateBasic() error {
	if ga.Name == "" {
		return fmt.Errorf("name cannot be empty")
	}
	if ga.InitialTokens <= 0 {
		return fmt.Errorf("initial tokens must be positive")
	}
	return nil
}

type Validator struct {
	GenesisAccount
	Stake int64

	// ConsensusKey is the key used by the validator to sign votes.
	ConsensusKey cryptotypes.PrivKey
}

// ValidateBasic performs stateless validation on the validitor
func (v *Validator) ValidateBasic() error {
	if err := v.GenesisAccount.ValidateBasic(); err != nil {
		return err
	}
	if v.Stake <= 0 {
		return fmt.Errorf("stake must be positive")
	}
	if v.ConsensusKey == nil {
		return fmt.Errorf("consensus key cannot be empty")
	}
	if v.Stake > v.InitialTokens {
		return fmt.Errorf("stake cannot be greater than initial tokens")
	}
	return nil
}

// GenTx generates a genesis transaction to create a validator as configured by
// the validator struct. It assumes the validator's genesis account has already
// been added to the keyring and that the sequence for that account is 0.
func (v *Validator) GenTx(ecfg encoding.Config, kr keyring.Keyring, chainID string) (sdk.Tx, error) {
	rec, err := kr.Key(v.Name)
	if err != nil {
		return nil, err
	}
	addr, err := rec.GetAddress()
	if err != nil {
		return nil, err
	}

	commission, err := sdk.NewDecFromStr("0.5")
	if err != nil {
		return nil, err
	}

	createValMsg, err := stakingtypes.NewMsgCreateValidator(
		sdk.ValAddress(addr),
		v.ConsensusKey.PubKey(),
		sdk.NewCoin(app.BondDenom, sdk.NewInt(v.Stake)),
		stakingtypes.NewDescription(v.Name, "", "", "", ""),
		stakingtypes.NewCommissionRates(commission, sdk.OneDec(), sdk.OneDec()),
		sdk.OneInt(),
	)
	if err != nil {
		return nil, err
	}

	fee := sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(1)))
	txBuilder := ecfg.TxConfig.NewTxBuilder()
	err = txBuilder.SetMsgs(createValMsg)
	if err != nil {
		return nil, err
	}
	txBuilder.SetFeeAmount(fee)    // Arbitrary fee
	txBuilder.SetGasLimit(1000000) // Need at least 100386

	txFactory := tx.Factory{}
	txFactory = txFactory.
		WithChainID(chainID).
		WithKeybase(kr).
		WithTxConfig(ecfg.TxConfig)

	err = tx.Sign(txFactory, v.Name, txBuilder, true)
	if err != nil {
		return nil, err
	}

	return txBuilder.GetTx(), nil
}

package genesis

import (
	"encoding/json"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	genutiltypes "github.com/cosmos/cosmos-sdk/x/genutil/types"
	tmrand "github.com/tendermint/tendermint/libs/rand"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
	coretypes "github.com/tendermint/tendermint/types"
)

// Genesis manages the creation of the genesis state of a network. It is meant
// to be used as the first step to any test that requires a network.
type Genesis struct {
	ecfg encoding.Config
	// ConsensusParams are the consensus parameters of the network.
	ConsensusParams *tmproto.ConsensusParams
	// ChainID is the chain ID of the network.
	ChainID string
	// GenesisTime is the genesis time of the network.
	GenesisTime time.Time

	// kr is the keyring used to generate the genesis accounts and validators.
	// Transaction keys for all genesis accounts are stored in this keyring and
	// are indexed by account name. Public keys and addresses can be derived
	// from those keys using the existing keyring API.
	kr keyring.Keyring

	// accounts are the genesis accounts that will be included in the genesis.
	accounts []Account
	// validators are the validators of the network. Note that each validator
	// also has a genesis account.
	validators []Validator
	// genTxs are the genesis transactions that will be included in the genesis.
	// Transactions are generated upon adding a validator to the genesis.
	genTxs []sdk.Tx
	genOps []Modifier
}

// NewDefaultGenesis creates a new default genesis with no accounts or validators.
func NewDefaultGenesis() *Genesis {
	ecfg := encoding.MakeConfig(app.ModuleBasics)
	g := &Genesis{
		ecfg:            ecfg,
		ConsensusParams: DefaultConsensusParams(),
		ChainID:         tmrand.Str(6),
		GenesisTime:     time.Now(),
		kr:              keyring.NewInMemory(ecfg.Codec),
		genOps:          []Modifier{},
	}
	return g
}

func (g *Genesis) WithModifiers(ops ...Modifier) *Genesis {
	g.genOps = append(g.genOps, ops...)
	return g
}

func (g *Genesis) WithConsensusParams(params *tmproto.ConsensusParams) *Genesis {
	g.ConsensusParams = params
	return g
}

func (g *Genesis) WithChainID(chainID string) *Genesis {
	g.ChainID = chainID
	return g
}

func (g *Genesis) WithGenesisTime(genesisTime time.Time) *Genesis {
	g.GenesisTime = genesisTime
	return g
}

func (g *Genesis) WithValidators(vals ...Validator) *Genesis {
	for _, val := range vals {
		err := g.AddValidator(val)
		if err != nil {
			panic(err)
		}
	}
	return g
}

func (g *Genesis) WithAccounts(accs ...Account) *Genesis {
	for _, acc := range accs {
		err := g.AddAccount(acc)
		if err != nil {
			panic(err)
		}
	}
	return g
}

func (g *Genesis) AddAccount(acc Account) error {
	_, err := g.kr.Key(acc.Name)
	if err == nil {
		return fmt.Errorf("account with name %s already exists", acc.Name)
	}
	if err := acc.ValidateBasic(); err != nil {
		return err
	}
	_, mn, err := g.kr.NewMnemonic(acc.Name, keyring.English, "", "", hd.Secp256k1)
	if err != nil {
		return err
	}
	acc.Mnemonic = mn
	if mn == "" {
		return fmt.Errorf("generated mnemonic for account %s is empty", acc.Name)
	}
	g.accounts = append(g.accounts, acc)
	return nil
}

func (g *Genesis) AddValidator(val Validator) error {
	if err := val.ValidateBasic(); err != nil {
		return err
	}

	// Add the validator's genesis account
	if err := g.AddAccount(val.Account); err != nil {
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

func (g *Genesis) Accounts() []Account {
	return g.accounts
}

// genAccountsToSDKTypes converts the genesis accounts to native SDK types.
func genAccountsToSDKTypes(kr keyring.Keyring, accs []Account) ([]banktypes.Balance, []authtypes.GenesisAccount, error) {
	genBals := make([]banktypes.Balance, len(accs))
	genAccs := make([]authtypes.GenesisAccount, len(accs))
	hasMap := make(map[string]bool)
	for i, acc := range accs {
		rec, err := kr.Key(acc.Name)
		if err != nil {
			return nil, nil, err
		}

		addr, err := rec.GetAddress()
		if err != nil {
			return nil, nil, err
		}

		if hasMap[addr.String()] {
			return nil, nil, fmt.Errorf("duplicate account address %s", addr)
		}
		hasMap[addr.String()] = true

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

	genBals, genAccs, err := genAccountsToSDKTypes(g.kr, g.Accounts())
	if err != nil {
		return nil, err
	}

	accounts, err := authtypes.PackAccounts(genAccs)
	if err != nil {
		return nil, err
	}
	authGenState.Accounts = append(authGenState.Accounts, accounts...)
	bankGenState.Balances = append(bankGenState.Balances, genBals...)
	bankGenState.Balances = banktypes.SanitizeGenesisBalances(bankGenState.Balances)

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

	for _, option := range g.genOps {
		state = option(state)
	}

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

func (g *Genesis) Validators() []Validator {
	return g.validators
}

// Validator returns the validator at the given index. False is returned if the
// index is out of bounds.
func (g *Genesis) Validator(i int) (Validator, bool) {
	if i < len(g.validators) {
		return g.validators[i], true
	}
	return Validator{}, false
}

func DefaultConsensusParams() *tmproto.ConsensusParams {
	cparams := coretypes.DefaultConsensusParams()
	cparams.Block.TimeIotaMs = 1
	cparams.Block.MaxBytes = appconsts.DefaultMaxBytes
	return cparams
}

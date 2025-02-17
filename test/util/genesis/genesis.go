package genesis

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/math/unsafe"
	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	testenc "github.com/celestiaorg/celestia-app/v4/test/util/encoding"
	tmproto "github.com/cometbft/cometbft/proto/tendermint/types"
	coretypes "github.com/cometbft/cometbft/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// Genesis manages the creation of the genesis state of a network. It is meant
// to be used as the first step to any test that requires a network.
type Genesis struct {
	// ecfg is the encoding configuration of the app.
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

// Accounts getter
func (g *Genesis) Accounts() []Account {
	return g.accounts
}

// Keyring getter
func (g *Genesis) Keyring() keyring.Keyring {
	return g.kr
}

// Validators getter
func (g *Genesis) Validators() []Validator {
	return g.validators
}

// NewDefaultGenesis creates a new default genesis with no accounts or validators.
func NewDefaultGenesis() *Genesis {
	enc := testenc.MakeTestConfig()
	g := &Genesis{
		ecfg:            enc,
		ConsensusParams: app.DefaultConsensusParams(),
		ChainID:         unsafe.Str(6),
		GenesisTime:     time.Now(),
		kr:              keyring.NewInMemory(enc.Codec),
		genOps:          []Modifier{},
	}
	return g
}

// WithModifiers adds a genesis modifier to the genesis.
func (g *Genesis) WithModifiers(ops ...Modifier) *Genesis {
	g.genOps = append(g.genOps, ops...)
	return g
}

// WithConsensusParams sets the consensus parameters of the genesis.
func (g *Genesis) WithConsensusParams(params *tmproto.ConsensusParams) *Genesis {
	g.ConsensusParams = params
	return g
}

// WithChainID sets the chain ID of the genesis.
func (g *Genesis) WithChainID(chainID string) *Genesis {
	g.ChainID = chainID
	return g
}

// WithGenesisTime sets the genesis time of the genesis.
func (g *Genesis) WithGenesisTime(genesisTime time.Time) *Genesis {
	g.GenesisTime = genesisTime
	return g
}

// WithValidators adds the given validators to the genesis.
func (g *Genesis) WithValidators(vals ...Validator) *Genesis {
	for _, val := range vals {
		err := g.NewValidator(val)
		if err != nil {
			panic(err)
		}
	}
	return g
}

// WithKeyringAccounts adds the given keyring accounts to the genesis. If an
// account with the same name already exists, it panics.
func (g *Genesis) WithKeyringAccounts(accs ...KeyringAccount) *Genesis {
	for _, acc := range accs {
		err := g.NewAccount(acc)
		if err != nil {
			panic(err)
		}
	}
	return g
}

func (g *Genesis) WithKeyring(kr keyring.Keyring) *Genesis {
	g.kr = kr
	return g
}

// AddAccount adds an existing account to the genesis.
func (g *Genesis) AddAccount(account Account) error {
	if err := account.ValidateBasic(); err != nil {
		return err
	}
	for _, acc := range g.accounts {
		if bytes.Equal(acc.PubKey.Bytes(), account.PubKey.Bytes()) {
			return fmt.Errorf("account with pubkey %s already exists", account.PubKey.String())
		}
	}
	g.accounts = append(g.accounts, account)
	return nil
}

// NewAccount creates a new account and adds it to the genesis.
func (g *Genesis) NewAccount(acc KeyringAccount) error {
	if err := acc.ValidateBasic(); err != nil {
		return err
	}
	// check that the account does not already exist
	if _, err := g.kr.Key(acc.Name); err == nil {
		return fmt.Errorf("account with name %s already exists", acc.Name)
	}

	// generate the keys and add it to the genesis keyring
	record, _, err := g.kr.NewMnemonic(acc.Name, keyring.English, "", "", hd.Secp256k1)
	if err != nil {
		return err
	}

	pubKey, err := record.GetPubKey()
	if err != nil {
		return err
	}

	account := Account{
		PubKey:  pubKey,
		Balance: acc.InitialTokens,
		Name:    acc.Name,
	}

	g.accounts = append(g.accounts, account)
	return nil
}

// AddValidator verifies and adds a given validator to the genesis.
func (g *Genesis) AddValidator(val Validator) error {
	if err := val.ValidateBasic(); err != nil {
		return err
	}

	g.validators = append(g.validators, val)
	return nil
}

// NewValidator creates a new validator account and adds it to the genesis.
func (g *Genesis) NewValidator(val Validator) error {
	// Add the validator's genesis account
	if err := g.NewAccount(val.KeyringAccount); err != nil {
		return err
	}

	return g.AddValidator(val)
}

// Export returns the genesis document of the network.
func (g *Genesis) Export() (*coretypes.GenesisDoc, error) {
	gentxs := make([]json.RawMessage, 0, len(g.genTxs))
	for _, val := range g.validators {
		genTx, err := val.GenTx(g.ecfg, g.kr, g.ChainID)
		if err != nil {
			return nil, err
		}

		bz, err := g.ecfg.TxConfig.TxJSONEncoder()(genTx)
		if err != nil {
			return nil, err
		}

		gentxs = append(gentxs, json.RawMessage(bz))
	}

	tempApp := app.New(log.NewNopLogger(), dbm.NewMemDB(), nil, 0, simtestutil.EmptyAppOptions{})

	return Document(
		tempApp.DefaultGenesis(),
		g.ecfg,
		g.ConsensusParams,
		g.ChainID,
		gentxs,
		g.accounts,
		g.GenesisTime,
		g.genOps...,
	)
}

// Validator returns the validator at the given index. False is returned if the
// index is out of bounds.
func (g *Genesis) Validator(i int) (Validator, bool) {
	if i < len(g.validators) {
		return g.validators[i], true
	}
	return Validator{}, false
}

func (g *Genesis) EncodingConfig() encoding.Config {
	return g.ecfg
}

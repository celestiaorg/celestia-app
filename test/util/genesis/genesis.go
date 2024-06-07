package genesis

import (
	"bytes"
	"encoding/json"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
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
		ConsensusParams: app.DefaultConsensusParams(),
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

func (g *Genesis) AddAccount(account Account) error {
	for _, acc := range g.accounts {
		if bytes.Equal(acc.PubKey.Bytes(), account.PubKey.Bytes()) {
			return fmt.Errorf("account with pubkey %s already exists", account.PubKey.String())
		}
	}
	g.accounts = append(g.accounts, account)
	return nil
}

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
	}

	g.accounts = append(g.accounts, account)
	return nil
}

func (g *Genesis) NewValidator(val Validator) error {
	if err := val.ValidateBasic(); err != nil {
		return err
	}

	// Add the validator's genesis account
	if err := g.NewAccount(val.KeyringAccount); err != nil {
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

// TODO improve this function to imply that we're just adding one validator to make it deterministic
func (g *Genesis) AddValidator(val Validator) error {
	mnemo := "body world north giggle crop reduce height copper damp next verify orphan lens loan adjust inform utility theory now ranch motion opinion crowd fun"
	rec, err := g.kr.NewAccount("validator1", mnemo, "", "", hd.Secp256k1)
	if err != nil {
		return err
	}
	validatorPubKey, err := rec.GetPubKey()
	if err != nil {
		return err
	}

	if err := val.ValidateBasic(); err != nil {
		return err
	}

	// make account from keyring account
	account := Account{
		PubKey:  validatorPubKey,
		Balance: val.KeyringAccount.InitialTokens,
	}

	if err := g.AddAccount(account); err != nil {
		return err
	}

	// TODO decide on this
	// add validator to genesis keyring
	// if _, err := g.kr.Key(val.Name); err == nil {
	// 	return fmt.Errorf("validator with name %s already exists", val.Name)
	// }

	// // Add the validator's genesis transaction
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

func (g *Genesis) Export() (*coretypes.GenesisDoc, error) {
	gentxs := make([]json.RawMessage, 0, len(g.genTxs))
	for _, genTx := range g.genTxs {
		bz, err := g.ecfg.TxConfig.TxJSONEncoder()(genTx)
		if err != nil {
			return nil, err
		}

		gentxs = append(gentxs, json.RawMessage(bz))
	}

	return Document(
		g.ecfg,
		g.ConsensusParams,
		g.ChainID,
		gentxs,
		g.accounts,
		g.genOps...,
	)
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
package genesis

import (
	"context"
	"errors"
	"fmt"
	mrand "math/rand"
	"time"

	"cosmossdk.io/math"
	"github.com/cometbft/cometbft/crypto"
	"github.com/cosmos/cosmos-sdk/client/tx"
	cryptocodec "github.com/cosmos/cosmos-sdk/crypto/codec"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"

	"github.com/celestiaorg/celestia-app/v4/app/encoding"
	"github.com/celestiaorg/celestia-app/v4/app/params"
)

const (
	DefaultInitialBalance = 1e15 // 1 billion TIA
)

// KeyringAccount represents a user account on the Celestia network.
// Either the name, if using the genesis keyring, or an address
// needs to be provided
type KeyringAccount struct {
	Name          string
	InitialTokens int64
}

func NewKeyringAccounts(initBal int64, names ...string) []KeyringAccount {
	accounts := make([]KeyringAccount, len(names))
	for i, name := range names {
		accounts[i] = KeyringAccount{
			Name:          name,
			InitialTokens: initBal,
		}
	}
	return accounts
}

func (ga *KeyringAccount) ValidateBasic() error {
	if ga.Name == "" {
		return errors.New("name cannot be empty")
	}
	if ga.InitialTokens <= 0 {
		return errors.New("initial tokens must be positive")
	}
	return nil
}

type Validator struct {
	KeyringAccount
	Stake int64

	// ConsensusKey is the key used by the validator to sign votes.
	ConsensusKey crypto.PrivKey
	NetworkKey   crypto.PrivKey
}

func NewDefaultValidator(name string) Validator {
	r := mrand.New(mrand.NewSource(time.Now().UnixNano()))
	return Validator{
		KeyringAccount: KeyringAccount{
			Name:          name,
			InitialTokens: DefaultInitialBalance,
		},
		Stake:        DefaultInitialBalance / 2, // save some tokens for fees
		ConsensusKey: GenerateEd25519(NewSeed(r)),
		NetworkKey:   GenerateEd25519(NewSeed(r)),
	}
}

// ValidateBasic performs stateless validation on the validator
func (v *Validator) ValidateBasic() error {
	if err := v.KeyringAccount.ValidateBasic(); err != nil {
		return err
	}
	if v.Stake <= 0 {
		return errors.New("stake must be positive")
	}
	if v.ConsensusKey == nil {
		return errors.New("consensus key cannot be empty")
	}
	if v.Stake > v.InitialTokens {
		return errors.New("stake cannot be greater than initial tokens")
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

	commission, err := math.LegacyNewDecFromStr("0.5")
	if err != nil {
		return nil, err
	}

	pk, err := cryptocodec.FromCmtPubKeyInterface(v.ConsensusKey.PubKey())
	if err != nil {
		return nil, fmt.Errorf("converting public key for node %s: %w", v.Name, err)
	}

	createValMsg, err := stakingtypes.NewMsgCreateValidator(
		sdk.ValAddress(addr).String(),
		pk,
		sdk.NewCoin(params.BondDenom, math.NewInt(v.Stake)),
		stakingtypes.NewDescription(v.Name, "", "", "", ""),
		stakingtypes.NewCommissionRates(commission, math.LegacyOneDec(), math.LegacyOneDec()),
		math.NewInt(v.Stake/2),
	)
	createValMsg.DelegatorAddress = sdk.ValAddress(addr).String()
	if err != nil {
		return nil, err
	}

	fee := sdk.NewCoins(sdk.NewCoin(params.BondDenom, math.NewInt(20000)))
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

	err = tx.Sign(context.TODO(), txFactory, v.Name, txBuilder, true)
	if err != nil {
		return nil, err
	}

	return txBuilder.GetTx(), nil
}

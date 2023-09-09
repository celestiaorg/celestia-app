package genesis

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/cosmos/cosmos-sdk/client/tx"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

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

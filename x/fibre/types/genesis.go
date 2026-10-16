package types

import (
	errorsmod "cosmossdk.io/errors"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// DefaultGenesis returns the default genesis state
func DefaultGenesis() *GenesisState {
	return &GenesisState{
		Params:            DefaultParams(),
		EscrowAccounts:    []EscrowAccount{},
		Withdrawals:       []Withdrawal{},
		ProcessedPayments: []ProcessedPayment{},
	}
}

// Validate performs basic genesis state validation returning an error upon any
// failure.
func (gs GenesisState) Validate() error {
	// Validate params
	if err := gs.Params.Validate(); err != nil {
		return err
	}

	// Validate escrow accounts
	signerMap := make(map[string]bool)
	for _, account := range gs.EscrowAccounts {
		if err := validateGenesisSigner(account.Signer); err != nil {
			return err
		}
		if signerMap[account.Signer] {
			return ErrDuplicateSigner
		}
		signerMap[account.Signer] = true

		if !account.Balance.IsValid() {
			return ErrInvalidBalance
		}
		if !account.AvailableBalance.IsValid() {
			return ErrInvalidBalance
		}
		if account.AvailableBalance.Amount.GT(account.Balance.Amount) {
			return ErrInvalidBalance
		}
	}

	// Validate withdrawals
	for _, withdrawal := range gs.Withdrawals {
		if err := validateGenesisSigner(withdrawal.Signer); err != nil {
			return err
		}
		if !withdrawal.Amount.IsValid() || !withdrawal.Amount.IsPositive() {
			return ErrInvalidAmount
		}
		if withdrawal.RequestedTimestamp.IsZero() {
			return ErrInvalidTimestamp
		}
	}

	// Validate processed payments
	hashMap := make(map[string]bool)
	for _, payment := range gs.ProcessedPayments {
		if len(payment.PaymentPromiseHash) == 0 {
			return ErrInvalidHash
		}
		hashStr := string(payment.PaymentPromiseHash)
		if hashMap[hashStr] {
			return ErrDuplicateHash
		}
		hashMap[hashStr] = true

		if payment.ProcessedAt.IsZero() {
			return ErrInvalidTimestamp
		}
	}

	return nil
}

// validateGenesisSigner requires a signer in the canonical bech32 form, which is
// what the keeper uses as the store key. The uppercase form of the same address
// would be stored under a different key that settlement never reads.
func validateGenesisSigner(signer string) error {
	addr, err := sdk.AccAddressFromBech32(signer)
	if err != nil {
		return errorsmod.Wrapf(ErrInvalidSigner, "%q: %s", signer, err)
	}
	if addr.String() != signer {
		return errorsmod.Wrapf(ErrInvalidSigner, "%q is not in canonical form %q", signer, addr.String())
	}
	return nil
}

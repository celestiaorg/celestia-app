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

	// Validate escrow accounts. Signers need not be canonical: InitGenesis
	// stores records under their raw signer and Migrate1to2 canonicalizes them,
	// merging case variants of the same address and refunding non-utia escrows.
	// An exact duplicate would overwrite the earlier record, so reject it.
	escrowDenoms := make(map[string]string, len(gs.EscrowAccounts))
	for _, account := range gs.EscrowAccounts {
		if err := ValidateGenesisSigner(account.Signer); err != nil {
			return err
		}
		if _, ok := escrowDenoms[account.Signer]; ok {
			return ErrDuplicateSigner
		}
		escrowDenoms[account.Signer] = account.Balance.Denom

		if !account.Balance.IsValid() {
			return ErrInvalidBalance
		}
		if !account.AvailableBalance.IsValid() {
			return ErrInvalidBalance
		}
		if account.Balance.Denom != account.AvailableBalance.Denom {
			return errorsmod.Wrapf(ErrInvalidBalance, "escrow %s balance and available balance denominations differ", account.Signer)
		}
		if account.AvailableBalance.Amount.GT(account.Balance.Amount) {
			return ErrInvalidBalance
		}
	}

	// Validate withdrawals with the same rules Migrate1to2 applies in InitGenesis.
	withdrawalKeys := make(map[string]bool, len(gs.Withdrawals))
	for _, withdrawal := range gs.Withdrawals {
		if err := ValidateGenesisSigner(withdrawal.Signer); err != nil {
			return err
		}
		if !withdrawal.Amount.IsValid() || !withdrawal.Amount.IsPositive() {
			return ErrInvalidAmount
		}
		if withdrawal.RequestedTimestamp.IsZero() || withdrawal.AvailableTimestamp.Before(withdrawal.RequestedTimestamp) {
			return ErrInvalidTimestamp
		}
		key := string(WithdrawalsBySignerKey(withdrawal.Signer, withdrawal.RequestedTimestamp))
		if withdrawalKeys[key] {
			return errorsmod.Wrapf(ErrDuplicateSigner, "duplicate withdrawal for %s requested at %s", withdrawal.Signer, withdrawal.RequestedTimestamp)
		}
		withdrawalKeys[key] = true
		// Pending withdrawals are reserved from the owning escrow's balance.
		if denom, ok := escrowDenoms[withdrawal.Signer]; !ok || denom != withdrawal.Amount.Denom {
			return errorsmod.Wrapf(ErrInvalidBalance, "withdrawal for %s has no escrow account in %s", withdrawal.Signer, withdrawal.Amount.Denom)
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

// ValidateGenesisSigner requires a well-formed bech32 signer. Upper case is
// accepted because v10 stored such signers verbatim; InitGenesis migrates them
// to the canonical lowercase form the keeper uses as its store key.
func ValidateGenesisSigner(signer string) error {
	if _, err := sdk.AccAddressFromBech32(signer); err != nil {
		return errorsmod.Wrapf(ErrInvalidSigner, "%q: %s", signer, err)
	}
	return nil
}

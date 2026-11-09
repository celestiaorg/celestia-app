package types

import (
	errorsmod "cosmossdk.io/errors"
	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
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
	if err := gs.Params.Validate(); err != nil {
		return err
	}
	return gs.ValidateRecords()
}

// ValidateRecords validates the genesis escrow accounts, withdrawals and
// processed payments, but not params. InitGenesis calls it before writing
// state, so it must reject every record set InitGenesis cannot store and
// migrate.
func (gs GenesisState) ValidateRecords() error {
	escrowDenoms, err := validateGenesisEscrows(gs.EscrowAccounts)
	if err != nil {
		return err
	}
	if err := validateGenesisWithdrawals(gs.Withdrawals, escrowDenoms); err != nil {
		return err
	}
	return validateGenesisProcessedPayments(gs.ProcessedPayments)
}

// validateGenesisEscrows validates escrow accounts and returns each raw
// signer's escrow denomination.
func validateGenesisEscrows(accounts []EscrowAccount) (map[string]string, error) {
	// Validate escrow accounts. Signers need not be canonical: InitGenesis
	// stores records under their raw signer and Migrate1to2 canonicalizes them,
	// merging case variants of the same address and refunding non-utia escrows.
	// An exact duplicate would overwrite the earlier record, so reject it.
	// Merged utia balances must fit in a math.Int, or the merge fails.
	escrowDenoms := make(map[string]string, len(accounts))
	mergedBalances := make(map[string]math.Int, len(accounts))
	for _, account := range accounts {
		canonical, err := canonicalGenesisSigner(account.Signer)
		if err != nil {
			return nil, err
		}
		if _, ok := escrowDenoms[account.Signer]; ok {
			return nil, ErrDuplicateSigner
		}
		escrowDenoms[account.Signer] = account.Balance.Denom

		if !account.Balance.IsValid() {
			return nil, ErrInvalidBalance
		}
		if !account.AvailableBalance.IsValid() {
			return nil, ErrInvalidBalance
		}
		if account.Balance.Denom != account.AvailableBalance.Denom {
			return nil, errorsmod.Wrapf(ErrInvalidBalance, "escrow %s balance and available balance denominations differ", account.Signer)
		}
		if account.AvailableBalance.Amount.GT(account.Balance.Amount) {
			return nil, ErrInvalidBalance
		}
		// AvailableBalance never exceeds Balance, so its merged sum fits too.
		if account.Balance.Denom == appconsts.BondDenom {
			merged, err := addMerged(mergedBalances, canonical, account.Balance.Amount)
			if err != nil {
				return nil, errorsmod.Wrapf(ErrInvalidBalance, "merged escrow balance for %s: %s", canonical, err)
			}
			mergedBalances[canonical] = merged
		}
	}
	return escrowDenoms, nil
}

// validateGenesisWithdrawals validates withdrawals with the same rules
// Migrate1to2 applies in InitGenesis.
func validateGenesisWithdrawals(withdrawals []Withdrawal, escrowDenoms map[string]string) error {
	withdrawalKeys := make(map[string]bool, len(withdrawals))
	mergedWithdrawals := make(map[string]math.Int, len(withdrawals))
	for _, withdrawal := range withdrawals {
		canonical, err := canonicalGenesisSigner(withdrawal.Signer)
		if err != nil {
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
		if withdrawal.Amount.Denom == appconsts.BondDenom {
			mergedKey := string(WithdrawalsBySignerKey(canonical, withdrawal.RequestedTimestamp))
			merged, err := addMerged(mergedWithdrawals, mergedKey, withdrawal.Amount.Amount)
			if err != nil {
				return errorsmod.Wrapf(ErrInvalidAmount, "merged withdrawal for %s requested at %s: %s", canonical, withdrawal.RequestedTimestamp, err)
			}
			mergedWithdrawals[mergedKey] = merged
		}
		// Pending withdrawals are reserved from the owning escrow's balance.
		if denom, ok := escrowDenoms[withdrawal.Signer]; !ok || denom != withdrawal.Amount.Denom {
			return errorsmod.Wrapf(ErrInvalidBalance, "withdrawal for %s has no escrow account in %s", withdrawal.Signer, withdrawal.Amount.Denom)
		}
	}
	return nil
}

// validateGenesisProcessedPayments rejects empty or duplicate hashes and
// missing timestamps.
func validateGenesisProcessedPayments(payments []ProcessedPayment) error {
	hashMap := make(map[string]bool)
	for _, payment := range payments {
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
	_, err := canonicalGenesisSigner(signer)
	return err
}

// canonicalGenesisSigner returns the canonical form Migrate1to2 stores signer under.
func canonicalGenesisSigner(signer string) (string, error) {
	addr, err := sdk.AccAddressFromBech32(signer)
	if err != nil {
		return "", errorsmod.Wrapf(ErrInvalidSigner, "%q: %s", signer, err)
	}
	return addr.String(), nil
}

// addMerged returns sums[key] + amount, failing where Migrate1to2's merge would.
func addMerged(sums map[string]math.Int, key string, amount math.Int) (math.Int, error) {
	sum, ok := sums[key]
	if !ok {
		return amount, nil
	}
	return sum.SafeAdd(amount)
}

package types

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
		if account.Signer == "" {
			return ErrInvalidSigner
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
		if withdrawal.Signer == "" {
			return ErrInvalidSigner
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

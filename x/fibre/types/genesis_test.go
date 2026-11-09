package types

import (
	"strings"
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestGenesisStateValidateSigner(t *testing.T) {
	signer := sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String()
	coin := sdk.NewCoin(appconsts.BondDenom, math.NewInt(1000))
	requested := time.Unix(1, 0).UTC()

	escrowGenesis := func(signer string) GenesisState {
		gs := *DefaultGenesis()
		gs.EscrowAccounts = []EscrowAccount{{Signer: signer, Balance: coin, AvailableBalance: coin}}
		return gs
	}
	withdrawalGenesis := func(signer string) GenesisState {
		gs := *DefaultGenesis()
		// The escrow is valid so that only the withdrawal's signer is under test.
		gs.EscrowAccounts = []EscrowAccount{{Signer: signer, Balance: coin, AvailableBalance: coin}}
		if _, err := sdk.AccAddressFromBech32(signer); err != nil {
			gs.EscrowAccounts[0].Signer = sdk.AccAddress([]byte("valid_escrow_signer_")).String()
		}
		gs.Withdrawals = []Withdrawal{{Signer: signer, Amount: coin, RequestedTimestamp: requested, AvailableTimestamp: requested}}
		return gs
	}
	// bech32 forbids mixing cases, so this is malformed rather than non-canonical.
	mixedCase := strings.ToUpper(signer[:12]) + signer[12:]

	tests := []struct {
		name    string
		genesis GenesisState
		wantErr bool
	}{
		{"escrow account with canonical signer", escrowGenesis(signer), false},
		{"escrow account with empty signer", escrowGenesis(""), true},
		{"escrow account with invalid signer", escrowGenesis("invalid-address"), true},
		{"escrow account with mixed-case signer", escrowGenesis(mixedCase), true},
		{"escrow account with uppercase signer", escrowGenesis(strings.ToUpper(signer)), false},
		{"withdrawal with canonical signer", withdrawalGenesis(signer), false},
		{"withdrawal with empty signer", withdrawalGenesis(""), true},
		{"withdrawal with invalid signer", withdrawalGenesis("invalid-address"), true},
		{"withdrawal with mixed-case signer", withdrawalGenesis(mixedCase), true},
		{"withdrawal with uppercase signer", withdrawalGenesis(strings.ToUpper(signer)), false},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.genesis.Validate()
			if tt.wantErr {
				require.ErrorIs(t, err, ErrInvalidSigner)
			} else {
				require.NoError(t, err)
			}
		})
	}
}

// TestGenesisStateValidateLegacyExport checks that Validate accepts a v10
// export that InitGenesis migrates: case variants of one address and escrows
// in other denominations.
func TestGenesisStateValidateLegacyExport(t *testing.T) {
	lower := sdk.AccAddress([]byte("genesis_signer______")).String()
	upper := strings.ToUpper(lower)
	other := sdk.AccAddress([]byte("genesis_other_signer")).String()
	requested := time.Unix(1, 0).UTC()

	gs := *DefaultGenesis()
	gs.EscrowAccounts = []EscrowAccount{
		{Signer: upper, Balance: sdk.NewInt64Coin("utia", 500), AvailableBalance: sdk.NewInt64Coin("utia", 400)},
		{Signer: lower, Balance: sdk.NewInt64Coin("utia", 50), AvailableBalance: sdk.NewInt64Coin("utia", 50)},
		{Signer: other, Balance: sdk.NewInt64Coin("stake", 70), AvailableBalance: sdk.NewInt64Coin("stake", 60)},
	}
	gs.Withdrawals = []Withdrawal{
		{Signer: upper, Amount: sdk.NewInt64Coin("utia", 100), RequestedTimestamp: requested, AvailableTimestamp: requested.Add(time.Hour)},
		{Signer: other, Amount: sdk.NewInt64Coin("stake", 10), RequestedTimestamp: requested, AvailableTimestamp: requested.Add(time.Hour)},
	}
	require.NoError(t, gs.Validate())
}

// TestGenesisStateValidateEscrowInvariants covers the escrow and withdrawal
// rules Migrate1to2 enforces in InitGenesis, so `genesis validate` rejects
// what InitChain would.
func TestGenesisStateValidateEscrowInvariants(t *testing.T) {
	signer := sdk.AccAddress([]byte("genesis_signer______")).String()
	other := sdk.AccAddress([]byte("genesis_other_signer")).String()
	coin := sdk.NewInt64Coin(appconsts.BondDenom, 1000)
	requested := time.Unix(1, 0).UTC()
	escrow := EscrowAccount{Signer: signer, Balance: coin, AvailableBalance: sdk.NewInt64Coin(appconsts.BondDenom, 900)}
	withdrawal := Withdrawal{Signer: signer, Amount: sdk.NewInt64Coin(appconsts.BondDenom, 100), RequestedTimestamp: requested, AvailableTimestamp: requested.Add(time.Hour)}

	tests := []struct {
		name    string
		mutate  func(gs *GenesisState)
		wantErr error
	}{
		{"valid", func(*GenesisState) {}, nil},
		{"duplicate escrow signer", func(gs *GenesisState) {
			gs.EscrowAccounts = append(gs.EscrowAccounts, escrow)
		}, ErrDuplicateSigner},
		{"escrow denominations differ", func(gs *GenesisState) {
			gs.EscrowAccounts[0].AvailableBalance = sdk.NewInt64Coin("stake", 900)
		}, ErrInvalidBalance},
		{"withdrawal without escrow", func(gs *GenesisState) {
			gs.Withdrawals[0].Signer = other
		}, ErrInvalidBalance},
		{"withdrawal escrow denomination differs", func(gs *GenesisState) {
			gs.Withdrawals[0].Amount = sdk.NewInt64Coin("stake", 100)
		}, ErrInvalidBalance},
		{"withdrawal available before requested", func(gs *GenesisState) {
			gs.Withdrawals[0].AvailableTimestamp = requested.Add(-time.Second)
		}, ErrInvalidTimestamp},
		{"duplicate withdrawal", func(gs *GenesisState) {
			gs.Withdrawals = append(gs.Withdrawals, withdrawal)
		}, ErrDuplicateSigner},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			gs := *DefaultGenesis()
			gs.EscrowAccounts = []EscrowAccount{escrow}
			gs.Withdrawals = []Withdrawal{withdrawal}
			tt.mutate(&gs)
			err := gs.Validate()
			if tt.wantErr == nil {
				require.NoError(t, err)
			} else {
				require.ErrorIs(t, err, tt.wantErr)
			}
		})
	}
}

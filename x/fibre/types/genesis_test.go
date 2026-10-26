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

	escrowGenesis := func(signer string) GenesisState {
		gs := *DefaultGenesis()
		gs.EscrowAccounts = []EscrowAccount{{Signer: signer, Balance: coin, AvailableBalance: coin}}
		return gs
	}
	withdrawalGenesis := func(signer string) GenesisState {
		gs := *DefaultGenesis()
		gs.Withdrawals = []Withdrawal{{Signer: signer, Amount: coin, RequestedTimestamp: time.Unix(1, 0).UTC()}}
		return gs
	}

	tests := []struct {
		name    string
		genesis GenesisState
		wantErr bool
	}{
		{"escrow account with canonical signer", escrowGenesis(signer), false},
		{"escrow account with empty signer", escrowGenesis(""), true},
		{"escrow account with invalid signer", escrowGenesis("invalid-address"), true},
		{"escrow account with uppercase signer", escrowGenesis(strings.ToUpper(signer)), true},
		{"withdrawal with canonical signer", withdrawalGenesis(signer), false},
		{"withdrawal with empty signer", withdrawalGenesis(""), true},
		{"withdrawal with invalid signer", withdrawalGenesis("invalid-address"), true},
		{"withdrawal with uppercase signer", withdrawalGenesis(strings.ToUpper(signer)), true},
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

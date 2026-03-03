package keeper_test

import (
	"context"

	"github.com/cometbft/cometbft/crypto/ed25519"
	sdk "github.com/cosmos/cosmos-sdk/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
)

// MockBankKeeper implements the expected BankKeeper interface for testing
type MockBankKeeper struct {
	SendCoinsFromAccountToModuleFn func(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error
}

func (m *MockBankKeeper) SendCoinsFromAccountToModule(ctx context.Context, senderAddr sdk.AccAddress, recipientModule string, amt sdk.Coins) error {
	if m.SendCoinsFromAccountToModuleFn != nil {
		return m.SendCoinsFromAccountToModuleFn(ctx, senderAddr, recipientModule, amt)
	}
	return nil
}

func (m *MockBankKeeper) SendCoinsFromModuleToAccount(ctx context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
	return nil
}

// MockStakingKeeper implements the expected StakingKeeper interface for testing
type MockStakingKeeper struct {
	historicalInfo map[int64]stakingtypes.HistoricalInfo
	validatorKeys  map[int64]ed25519.PrivKey
}

func (m *MockStakingKeeper) GetHistoricalInfo(ctx context.Context, height int64) (stakingtypes.HistoricalInfo, error) {
	if m.historicalInfo != nil {
		if info, ok := m.historicalInfo[height]; ok {
			return info, nil
		}
	}
	return stakingtypes.HistoricalInfo{}, nil
}

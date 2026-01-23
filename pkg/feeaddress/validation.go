package feeaddress

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ValidateProtocolFee validates the fee for a protocol fee transaction.
// It checks that the fee has exactly one coin in the native denom with a positive amount.
// If expectedAmount is non-nil, it also validates that the fee equals the expected amount.
func ValidateProtocolFee(fee sdk.Coins, expectedAmount *sdk.Coin) error {
	if len(fee) != 1 {
		return fmt.Errorf("protocol fee tx requires exactly one fee coin, got %d", len(fee))
	}
	if fee[0].Denom != appconsts.BondDenom {
		return fmt.Errorf("protocol fee tx requires %s denom, got %s", appconsts.BondDenom, fee[0].Denom)
	}
	if !fee[0].Amount.IsPositive() {
		return fmt.Errorf("protocol fee tx requires positive amount, got %s", fee[0].Amount)
	}
	if expectedAmount != nil && !fee[0].Equal(*expectedAmount) {
		return fmt.Errorf("fee %s does not equal expected fee %s", fee[0], expectedAmount)
	}
	return nil
}

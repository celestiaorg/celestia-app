package types

import (
	"fmt"

	"github.com/celestiaorg/celestia-app/v7/pkg/appconsts"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

// ValidateBasic performs stateless validation of MsgBurn
func (msg *MsgBurn) ValidateBasic() error {
	_, err := sdk.AccAddressFromBech32(msg.Signer)
	if err != nil {
		return fmt.Errorf("invalid signer address: %w", err)
	}

	if msg.Amount.Denom != appconsts.BondDenom {
		return fmt.Errorf("only %s can be burned, got %s", appconsts.BondDenom, msg.Amount.Denom)
	}

	if !msg.Amount.IsPositive() {
		return fmt.Errorf("burn amount must be positive")
	}

	return nil
}

package types

import (
	"fmt"

	sdk "github.com/cosmos/cosmos-sdk/types"
)

var _ sdk.Msg = &MsgUpdateParams{}

// ValidateBasic performs stateless validation of MsgUpdateParams: it verifies
// the authority is a well-formed bech32 address and delegates to Params.Validate
// for per-field range checks.
func (m MsgUpdateParams) ValidateBasic() error {
	if _, err := sdk.AccAddressFromBech32(m.Authority); err != nil {
		return fmt.Errorf("invalid authority address: %w", err)
	}
	return m.Params.Validate()
}

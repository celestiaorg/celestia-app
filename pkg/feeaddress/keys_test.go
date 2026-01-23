package feeaddress

import (
	"testing"

	_ "github.com/celestiaorg/celestia-app/v7/app/params" // import for bech32 prefix init
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/assert"
)

// TestFeeAddressMatchesExpected verifies that the module account address
// derived from ModuleName matches the expected address.
func TestFeeAddressMatchesExpected(t *testing.T) {
	expected := "celestia18sjk23yldd9dg7j33sk24elwz2f06zt7ahx39y"
	got := authtypes.NewModuleAddress(ModuleName).String()
	assert.Equal(t, expected, got, "fee address module account should match expected address")
}

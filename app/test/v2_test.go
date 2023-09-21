package app

import (
	"testing"

	vestingtypesv2 "github.com/cosmos/cosmos-sdk/v2/x/auth/vesting/types"
)

func TestVersion(t *testing.T) {
	_ = vestingtypesv2.MsgCreatePeriodicVestingAccount{}
}

package minfee

import (
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/assert"
)

func TestDefaultNetworkMinGasPriceAsDec(t *testing.T) {
	want, err := sdk.NewDecFromStr("0.000001")
	assert.NoError(t, err)

	got := DefaultNetworkMinGasPriceAsDec()
	assert.Equal(t, want, got)
}

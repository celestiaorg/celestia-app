package minfee_test

import (
	"testing"

	minfee "github.com/celestiaorg/celestia-app/x/minfee"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

func TestValidateMinGasPrice(t *testing.T) {
    // Test valid min gas price type (sdk.Dec)
    validMinGasPrice := sdk.NewDec(1)
    err := minfee.ValidateMinGasPrice(validMinGasPrice)
    require.NoError(t, err)

    // Test invalid min gas price
    invalidMinGasPrice := 1
    err = minfee.ValidateMinGasPrice(invalidMinGasPrice)
    require.Error(t, err)
}

func TestValidateGenesis(t *testing.T) {
    // Test valid genesis state
    validGenesis := &minfee.GenesisState{
        GlobalMinGasPrice: 1,
    }
    err := minfee.ValidateGenesis(validGenesis)
    require.NoError(t, err)

    // Test invalid genesis state
    invalidGenesis := &minfee.GenesisState{
        GlobalMinGasPrice: -1,
    }
    err = minfee.ValidateGenesis(invalidGenesis)
    require.Error(t, err)
}
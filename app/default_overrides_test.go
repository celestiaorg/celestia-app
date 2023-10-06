package app

import (
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/assert"
)

// Test_newGovModule verifies that the gov module's genesis state has defaults
// overridden.
func Test_newGovModule(t *testing.T) {
	encCfg := encoding.MakeConfig(ModuleEncodingRegisters...)
	day := time.Duration(time.Hour * 24)
	oneWeek := day * 7
	twoWeeks := oneWeek * 2

	govModule := newGovModule()
	raw := govModule.DefaultGenesis(encCfg.Codec)
	govGenesisState := govtypes.GenesisState{}

	encCfg.Codec.MustUnmarshalJSON(raw, &govGenesisState)

	want := []types.Coin{{
		Denom:  BondDenom,
		Amount: types.NewInt(1000000000),
	}}

	assert.Equal(t, want, govGenesisState.DepositParams.MinDeposit)
	assert.Equal(t, oneWeek, *govGenesisState.DepositParams.MaxDepositPeriod)
	assert.Equal(t, twoWeeks, *govGenesisState.VotingParams.VotingPeriod)
}

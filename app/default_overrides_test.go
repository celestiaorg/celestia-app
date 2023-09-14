package app

import (
	"encoding/json"
	"testing"

	"github.com/celestiaorg/celestia-app/app/encoding"
	"github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/assert"
)

// Test_newGovModule tests that the default genesis state for the gov module
// uses the utia denominiation.
func Test_newGovModule(t *testing.T) {
	encCfg := encoding.MakeConfig(ModuleEncodingRegisters...)

	govModule := newGovModule()
	raw := govModule.DefaultGenesis(encCfg.Codec)
	govGenesisState := govtypes.GenesisState{}

	// HACKHACK explicitly ignore the error returned from json.Unmarshal because
	// the error is a failure to unmarshal the string StartingProposalId as a
	// uint which is unrelated to the test here.
	_ = json.Unmarshal(raw, &govGenesisState)

	want := []types.Coin{{
		Denom:  BondDenom,
		Amount: types.NewInt(1000000000),
	}}

	assert.Equal(t, want, govGenesisState.DepositParams.MinDeposit)
}

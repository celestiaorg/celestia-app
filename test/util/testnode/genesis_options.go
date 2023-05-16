package testnode

import (
	"encoding/json"
	"fmt"

	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/codec"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
)

// GenesisOption allows for arbitrary changes to be make the genesis state after
// initial accounts have been added. It accpets the genesis state as input and
// it expected to return it as output.
type GenesisOption func(state map[string]json.RawMessage) map[string]json.RawMessage

// SetBlobParams will set the provided blob params as genesis state.
func SetBlobParams(codec codec.Codec, params blobtypes.Params) GenesisOption {
	// use the minimum data commitment window (100)
	return func(state map[string]json.RawMessage) map[string]json.RawMessage {
		fmt.Println("setting blob params")
		fmt.Println("params", params, "state", len(state))
		blobGenState := blobtypes.DefaultGenesis()
		blobGenState.Params = params
		state[blobtypes.ModuleName] = codec.MustMarshalJSON(blobGenState)
		return state
	}
}

// ImmediateProposals sets the thresholds for getting a gov proposal to very low
// levels.
func ImmediateProposals(codec codec.Codec) GenesisOption {
	return func(state map[string]json.RawMessage) map[string]json.RawMessage {
		gs := v1.DefaultGenesisState()
		gs.TallyParams.Quorum = "0.000001"
		gs.TallyParams.Threshold = "0.000001"
		state[govtypes.ModuleName] = codec.MustMarshalJSON(gs)
		return state
	}
}

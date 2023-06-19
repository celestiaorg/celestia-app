package testnode

import (
	"encoding/json"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	blobtypes "github.com/celestiaorg/celestia-app/x/blob/types"
	"github.com/cosmos/cosmos-sdk/codec"
	sdk "github.com/cosmos/cosmos-sdk/types"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
)

// GenesisOption allows for arbitrary changes to be made on the genesis state
// after initial accounts have been added. It accepts the genesis state as input
// and is expected to return the modifed genesis as output.
type GenesisOption func(state map[string]json.RawMessage) map[string]json.RawMessage

// SetBlobParams will set the provided blob params as genesis state.
func SetBlobParams(codec codec.Codec, params blobtypes.Params) GenesisOption {
	return func(state map[string]json.RawMessage) map[string]json.RawMessage {
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
		gs.DepositParams.MinDeposit = sdk.NewCoins(sdk.NewCoin(app.BondDenom, sdk.NewInt(1)))
		gs.TallyParams.Quorum = "0.000001"
		gs.TallyParams.Threshold = "0.000001"
		vp := time.Second * 5
		gs.VotingParams.VotingPeriod = &vp
		state[govtypes.ModuleName] = codec.MustMarshalJSON(gs)
		return state
	}
}

package genesis_test

import (
	"encoding/json"
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/test/util/genesis"
	govtypes "github.com/cosmos/cosmos-sdk/x/gov/types"
	govv1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	"github.com/stretchr/testify/require"
)

// TestImmediateProposalsValidParams checks that the gov params set by
// ImmediateProposals pass validation, which genesis from-export runs on an
// export of a network started with them.
func TestImmediateProposalsValidParams(t *testing.T) {
	cdc := encoding.MakeConfig(app.ModuleEncodingRegisters...).Codec
	state := genesis.ImmediateProposals(cdc)(map[string]json.RawMessage{})

	var gs govv1.GenesisState
	require.NoError(t, cdc.UnmarshalJSON(state[govtypes.ModuleName], &gs))
	require.NoError(t, gs.Params.ValidateBasic())
}

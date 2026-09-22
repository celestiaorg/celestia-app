package main

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/stretchr/testify/require"
)

func TestFibreBudgetOverridePreservesFiveMinutePreset(t *testing.T) {
	state := fibreParamsModifier(1<<40, 0)(map[string]json.RawMessage{})
	var genesis fibretypes.GenesisState
	codec := encoding.MakeConfig(app.ModuleEncodingRegisters...).Codec
	require.NoError(t, codec.UnmarshalJSON(state[fibretypes.ModuleName], &genesis))
	require.NoError(t, genesis.Validate())
	require.Equal(t, 5*time.Minute, genesis.Params.PaymentPromiseTimeout)
	require.Equal(t, 5*time.Minute, genesis.Params.ShardRetention)
	require.Equal(t, uint64(1<<40), genesis.Params.FullStakeStorageBudget)
}

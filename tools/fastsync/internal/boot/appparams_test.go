package boot

import (
	"testing"
	"time"

	"github.com/cometbft/cometbft/types"
	"github.com/stretchr/testify/require"
)

func TestLegacyAppParams(t *testing.T) {
	p, err := legacyAppParams(
		[]byte(`{"max_bytes":"1974272","max_gas":"-1"}`),
		[]byte(`{"max_age_num_blocks":"120960","max_age_duration":"1814400000000000","max_bytes":"1048576"}`),
		[]byte(`{"pub_key_types":["ed25519"]}`),
		[]byte(`{"app_version":"2"}`),
	)
	require.NoError(t, err)
	want := types.ConsensusParams{
		Block:     types.BlockParams{MaxBytes: 1974272, MaxGas: -1},
		Evidence:  types.EvidenceParams{MaxAgeNumBlocks: 120960, MaxAgeDuration: 504 * time.Hour, MaxBytes: 1048576},
		Validator: types.ValidatorParams{PubKeyTypes: []string{"ed25519"}},
		Version:   types.VersionParams{App: 2},
	}
	require.NoError(t, p.Check(want))
	want.Version.App = 3
	require.ErrorContains(t, p.Check(want), "app version")

	_, err = legacyAppParams(nil, nil, nil, nil)
	require.Error(t, err)
	_, err = legacyAppParams([]byte(`{"max_bytes":"x"}`), []byte(`{}`), []byte(`{}`), nil)
	require.Error(t, err)
}

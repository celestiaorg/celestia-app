package app

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/baseapp"
	storetypes "github.com/cosmos/cosmos-sdk/store/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"github.com/tendermint/tendermint/libs/log"
	dbm "github.com/tendermint/tm-db"
)

type mapAppOptions map[string]any

func (m mapAppOptions) Get(key string) any { return m[key] }

func TestSignaturePreverifyEnabled(t *testing.T) {
	require.True(t, signaturePreverifyEnabled(mapAppOptions{}))
	require.True(t, signaturePreverifyEnabled(mapAppOptions{FlagSignaturePreverify: true}))
	require.True(t, signaturePreverifyEnabled(mapAppOptions{FlagSignaturePreverify: "true"}))
	require.False(t, signaturePreverifyEnabled(mapAppOptions{FlagSignaturePreverify: false}))
	require.False(t, signaturePreverifyEnabled(mapAppOptions{FlagSignaturePreverify: "false"}))
	require.Panics(t, func() { signaturePreverifyEnabled(mapAppOptions{FlagSignaturePreverify: "nope"}) })
}

// parallelCommitRecorder records the value baseapp passes to the multistore.
type parallelCommitRecorder struct {
	storetypes.CommitMultiStore
	got *bool
}

func (r parallelCommitRecorder) SetParallelCommit(enabled bool) { *r.got = enabled }

// TestPerformanceBaseappOptions checks that the app.toml values, including an
// explicit false, reach the multistore and the SDK execution-cache switch.
func TestPerformanceBaseappOptions(t *testing.T) {
	t.Cleanup(func() { sdk.SetExecutionCaches(true) })
	for _, tc := range []struct {
		opts mapAppOptions
		want bool
	}{
		{mapAppOptions{}, true},
		{mapAppOptions{FlagParallelCommit: true, FlagExecutionCaches: "true"}, true},
		{mapAppOptions{FlagParallelCommit: false, FlagExecutionCaches: "false"}, false},
	} {
		bapp := baseapp.NewBaseApp("test", log.NewNopLogger(), dbm.NewMemDB(), nil)
		got := !tc.want
		bapp.SetCMS(parallelCommitRecorder{bapp.CommitMultiStore(), &got})
		sdk.SetExecutionCaches(!tc.want)
		for _, opt := range PerformanceBaseappOptions(tc.opts) {
			opt(bapp)
		}
		require.Equal(t, tc.want, got, "parallel-commit %v", tc.opts)
		require.Equal(t, tc.want, sdk.ExecutionCachesEnabled(), "execution-caches %v", tc.opts)
	}
}

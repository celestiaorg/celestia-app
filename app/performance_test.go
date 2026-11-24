package app

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/server"
	"github.com/cosmos/cosmos-sdk/store/rootmulti"
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

// storeKnobRecorder records the deferred-persistence and repair-ahead values
// baseapp passes to the multistore.
type storeKnobRecorder struct {
	storetypes.CommitMultiStore
	mode        string
	interval    int64
	snapshot    uint64
	repairAhead *bool
}

func (r *storeKnobRecorder) SetDeferredPersistence(mode string, interval int64, snapshotInterval uint64) error {
	r.mode, r.interval, r.snapshot = mode, interval, snapshotInterval
	return nil
}

func (r *storeKnobRecorder) SetRepairAhead(enabled bool) { r.repairAhead = &enabled }

// TestPerformanceBaseappOptionsStore checks that deferred persistence and
// store repair follow app.toml and that a read-only load (export, rollback)
// turns both off.
func TestPerformanceBaseappOptionsStore(t *testing.T) {
	t.Cleanup(func() { sdk.SetExecutionCaches(true) })
	for _, tc := range []struct {
		name         string
		opts         mapAppOptions
		wantMode     string
		wantInterval int64
		wantSnapshot uint64
		wantRepair   bool
	}{
		{"defaults", mapAppOptions{}, rootmulti.DeferredSync, rootmulti.DefaultDeferredFlushInterval, 0, true},
		{"explicit off", mapAppOptions{FlagDeferredPersistence: "off", FlagDeferredFlushInterval: 7}, rootmulti.DeferredOff, 7, 0, true},
		{"snapshot interval passed through", mapAppOptions{server.FlagStateSyncSnapshotInterval: 1500}, rootmulti.DeferredSync, rootmulti.DefaultDeferredFlushInterval, 1500, true},
		{"read-only load", mapAppOptions{server.KeyReadOnlyLoad: true, FlagDeferredPersistence: "sync"}, rootmulti.DeferredOff, rootmulti.DefaultDeferredFlushInterval, 0, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			bapp := baseapp.NewBaseApp("test", log.NewNopLogger(), dbm.NewMemDB(), nil)
			rec := &storeKnobRecorder{CommitMultiStore: bapp.CommitMultiStore()}
			bapp.SetCMS(rec)
			for _, opt := range PerformanceBaseappOptions(tc.opts) {
				opt(bapp)
			}
			require.Equal(t, tc.wantMode, rec.mode)
			require.Equal(t, tc.wantInterval, rec.interval)
			require.Equal(t, tc.wantSnapshot, rec.snapshot)
			require.NotNil(t, rec.repairAhead)
			require.Equal(t, tc.wantRepair, *rec.repairAhead)
		})
	}
}

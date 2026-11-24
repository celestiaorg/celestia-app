package app

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/server"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/cosmos/cosmos-sdk/store/rootmulti"
	"github.com/spf13/cast"
)

// app.toml [performance] keys read by this app. A missing bool key means enabled.
const (
	// FlagSignaturePreverify enables verifying a DeliverTx batch's signatures
	// in parallel before execution.
	FlagSignaturePreverify = "performance.signature-preverify"
	// FlagParallelCommit makes the multistore write and commit its stores
	// concurrently.
	FlagParallelCommit = "performance.parallel-commit"
	// FlagExecutionCaches enables the gas-identical execution caches.
	FlagExecutionCaches = "performance.execution-caches"
	// FlagDeferredPersistence is "sync" (the default) or "off"; see
	// rootmulti.Store.SetDeferredPersistence.
	FlagDeferredPersistence = server.FlagDeferredPersistence
	// FlagDeferredFlushInterval is the number of blocks between deferred
	// flushes.
	FlagDeferredFlushInterval = server.FlagDeferredFlushInterval
)

// boolDefaultTrue reads a bool app option, defaulting to true when it is
// unset. It panics on a value that is not a bool, like other invalid options.
func boolDefaultTrue(appOpts servertypes.AppOptions, key string) bool {
	raw := appOpts.Get(key)
	if raw == nil {
		return true
	}
	enabled, err := cast.ToBoolE(raw)
	if err != nil {
		panic(fmt.Sprintf("invalid %s %q: %v", key, raw, err))
	}
	return enabled
}

// signaturePreverifyEnabled reads FlagSignaturePreverify, defaulting to true.
func signaturePreverifyEnabled(appOpts servertypes.AppOptions) bool {
	return boolDefaultTrue(appOpts, FlagSignaturePreverify)
}

// PerformanceBaseappOptions returns the baseapp options for the [performance]
// keys, mirroring server.DefaultBaseappOptions, which this app does not use.
// Bool keys default to true, so an explicit false in app.toml turns them off.
// Commands that only read the DB (export, rollback) set server.KeyReadOnlyLoad:
// their load must not repair stores, use deferred persistence or write the
// IAVL fast-node index.
func PerformanceBaseappOptions(appOpts servertypes.AppOptions) []func(*baseapp.BaseApp) {
	readOnly := cast.ToBool(appOpts.Get(server.KeyReadOnlyLoad))
	opts := []func(*baseapp.BaseApp){
		baseapp.SetSignaturePreverify(signaturePreverifyEnabled(appOpts)),
		baseapp.SetParallelCommit(boolDefaultTrue(appOpts, FlagParallelCommit)),
		baseapp.SetExecutionCaches(boolDefaultTrue(appOpts, FlagExecutionCaches)),
		baseapp.SetStoreRepairAhead(!readOnly),
		baseapp.SetDeferredPersistence(deferredPersistenceOptions(appOpts, readOnly)),
	}
	if readOnly {
		opts = append(opts, baseapp.SetIAVLDisableFastNode(true))
	}
	return opts
}

// deferredPersistenceOptions reads FlagDeferredPersistence and
// FlagDeferredFlushInterval (missing keys mean the store defaults) and the
// state sync snapshot interval, which keeps deferred persistence off when set.
// A read-only load never uses deferred persistence.
func deferredPersistenceOptions(appOpts servertypes.AppOptions, readOnly bool) (string, int64, uint64) {
	mode := rootmulti.DeferredSync
	if v := appOpts.Get(FlagDeferredPersistence); v != nil {
		mode = cast.ToString(v)
	}
	if readOnly {
		mode = rootmulti.DeferredOff
	}
	interval := int64(rootmulti.DefaultDeferredFlushInterval)
	if v := appOpts.Get(FlagDeferredFlushInterval); v != nil {
		interval = cast.ToInt64(v)
	}
	return mode, interval, cast.ToUint64(appOpts.Get(server.FlagStateSyncSnapshotInterval))
}

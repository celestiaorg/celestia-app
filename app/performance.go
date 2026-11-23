package app

import (
	"fmt"

	"github.com/cosmos/cosmos-sdk/baseapp"
	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cast"
)

// app.toml [performance] keys read by this app. A missing key means enabled.
const (
	// FlagSignaturePreverify enables verifying a DeliverTx batch's signatures
	// in parallel before execution.
	FlagSignaturePreverify = "performance.signature-preverify"
	// FlagParallelCommit makes the multistore write and commit its stores
	// concurrently.
	FlagParallelCommit = "performance.parallel-commit"
	// FlagExecutionCaches enables the gas-identical execution caches.
	FlagExecutionCaches = "performance.execution-caches"
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
// keys that the SDK applies through baseapp, each defaulting to true.
func PerformanceBaseappOptions(appOpts servertypes.AppOptions) []func(*baseapp.BaseApp) {
	return []func(*baseapp.BaseApp){
		baseapp.SetParallelCommit(boolDefaultTrue(appOpts, FlagParallelCommit)),
		baseapp.SetExecutionCaches(boolDefaultTrue(appOpts, FlagExecutionCaches)),
	}
}

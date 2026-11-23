package app

import (
	"fmt"

	servertypes "github.com/cosmos/cosmos-sdk/server/types"
	"github.com/spf13/cast"
)

// FlagSignaturePreverify is the app.toml key that enables verifying a DeliverTx
// batch's signatures in parallel before execution. A missing key means enabled.
const FlagSignaturePreverify = "performance.signature-preverify"

// signaturePreverifyEnabled reads FlagSignaturePreverify, defaulting to true.
// It panics on a value that is not a bool, like other invalid app options.
func signaturePreverifyEnabled(appOpts servertypes.AppOptions) bool {
	raw := appOpts.Get(FlagSignaturePreverify)
	if raw == nil {
		return true
	}
	enabled, err := cast.ToBoolE(raw)
	if err != nil {
		panic(fmt.Sprintf("invalid %s %q: %v", FlagSignaturePreverify, raw, err))
	}
	return enabled
}

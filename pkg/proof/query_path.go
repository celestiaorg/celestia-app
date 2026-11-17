package proof

import "github.com/cosmos/cosmos-sdk/baseapp"

// IsProofQueryPath reports whether an ABCI query path routes to an inclusion
// proof querier, parsing it the same way BaseApp.Query does.
func IsProofQueryPath(path string) bool {
	parts := baseapp.SplitABCIQueryPath(path)
	if len(parts) < 2 || parts[0] != baseapp.QueryPathCustom {
		return false
	}
	return parts[1] == TxInclusionQueryPath || parts[1] == ShareInclusionQueryPath
}

package module

import (
	"sort"

	sdkmodule "github.com/cosmos/cosmos-sdk/types/module"
)

// defaultMigrationsOrder returns a default migrations order. The order is
// ascending alphabetical by module name except "auth" will will be last. See:
// https://github.com/cosmos/cosmos-sdk/issues/10591
func defaultMigrationsOrder(modules []string) []string {
	const authName = "auth"
	out := make([]string, 0, len(modules))
	hasAuth := false
	for _, m := range modules {
		if m == authName {
			hasAuth = true
		} else {
			out = append(out, m)
		}
	}
	sort.Strings(out)
	if hasAuth {
		out = append(out, authName)
	}
	return out
}

func getKeys(m map[uint64]map[string]sdkmodule.AppModule) []uint64 {
	keys := make([]uint64, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

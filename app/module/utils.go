package module

import (
	"slices"
	"sort"

	sdkmodule "github.com/cosmos/cosmos-sdk/types/module"
)

const authName = "auth"

// defaultMigrationsOrder returns a default migrations order. The order is
// ascending alphabetical by module name except "auth" will will be last. See:
// https://github.com/cosmos/cosmos-sdk/issues/10591
func defaultMigrationsOrder(modules []string) []string {
	result := filter(modules, isNotAuth)
	sort.Strings(result)

	if hasAuth := slices.Contains(modules, authName); hasAuth {
		return append(result, authName)
	}
	return result
}

func filter(elements []string, filter func(string) bool) (filtered []string) {
	for _, element := range elements {
		if filter(element) {
			filtered = append(filtered, element)
		}
	}
	return filtered
}

func isNotAuth(name string) bool {
	return name != "auth"
}

func getKeys(m map[uint64]map[string]sdkmodule.AppModule) []uint64 {
	keys := make([]uint64, 0, len(m))
	for key := range m {
		keys = append(keys, key)
	}
	return keys
}

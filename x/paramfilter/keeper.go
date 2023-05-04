package paramfilter

import (
	"fmt"
)

type Keeper struct {
	forbiddenParams map[string]bool
}

func NewKeeper(forbiddenParams ...[2]string) Keeper {
	consolidatedParams := make(map[string]bool, len(forbiddenParams))
	for _, param := range forbiddenParams {
		consolidatedParams[fmt.Sprintf("%s-%s", param[0], param[1])] = true
	}
	return Keeper{forbiddenParams: consolidatedParams}
}

func (k Keeper) IsForbidden(subspace string, key string) bool {
	return k.forbiddenParams[fmt.Sprintf("%s-%s", subspace, key)]
}

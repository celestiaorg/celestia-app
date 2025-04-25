package testnode

import (
	"github.com/celestiaorg/celestia-app/v4/test/util/random"
)

// RandomAccounts returns a list of n random accounts
func RandomAccounts(n int) (accounts []string) {
	for i := 0; i < n; i++ {
		account := random.Str(20)
		accounts = append(accounts, account)
	}
	return accounts
}

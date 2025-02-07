package testnode

import tmrand "cosmossdk.io/math/unsafe"

// RandomAccounts returns a list of n random accounts
func RandomAccounts(n int) (accounts []string) {
	for i := 0; i < n; i++ {
		account := tmrand.Str(20)
		accounts = append(accounts, account)
	}
	return accounts
}

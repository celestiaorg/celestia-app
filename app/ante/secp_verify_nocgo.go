//go:build !cgo || gofuzz

package ante

import cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"

func txVerificationPubKey(key cryptotypes.PubKey) cryptotypes.PubKey {
	return key
}

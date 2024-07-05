package testfactory

import (
	"bytes"
	"sort"

	"github.com/celestiaorg/celestia-app/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/pkg/namespace"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	sdk "github.com/cosmos/cosmos-sdk/types"
	gethcommon "github.com/ethereum/go-ethereum/common"
	tmrand "github.com/tendermint/tendermint/libs/rand"
)

func Repeat[T any](s T, count int) []T {
	ss := make([]T, count)
	for i := 0; i < count; i++ {
		ss[i] = s
	}
	return ss
}

// GenerateRandNamespacedRawData returns random data of length count. Each chunk
// of random data is of size shareSize and is prefixed with a random blob
// namespace.
func GenerateRandNamespacedRawData(count int) (result [][]byte) {
	for i := 0; i < count; i++ {
		rawData := tmrand.Bytes(appconsts.ShareSize)
		namespace := namespace.RandomBlobNamespace().Bytes()
		copy(rawData, namespace)
		result = append(result, rawData)
	}

	sortByteArrays(result)
	return result
}

func sortByteArrays(src [][]byte) {
	sort.Slice(src, func(i, j int) bool { return bytes.Compare(src[i], src[j]) < 0 })
}

func RandomAccountNames(count int) []string {
	accounts := make([]string, 0, count)
	for i := 0; i < count; i++ {
		accounts = append(accounts, tmrand.Str(10))
	}
	return accounts
}

func RandomEVMAddress() gethcommon.Address {
	return gethcommon.BytesToAddress(tmrand.Bytes(gethcommon.AddressLength))
}

func GetAddress(keys keyring.Keyring, account string) sdk.AccAddress {
	rec, err := keys.Key(account)
	if err != nil {
		panic(err)
	}
	addr, err := rec.GetAddress()
	if err != nil {
		panic(err)
	}
	return addr
}

func GetAddresses(keys keyring.Keyring) []sdk.AccAddress {
	recs, err := keys.List()
	if err != nil {
		panic(err)
	}
	addresses := make([]sdk.AccAddress, 0, len(recs))
	for _, rec := range recs {
		address, err := rec.GetAddress()
		if err != nil {
			panic(err)
		}
		addresses = append(addresses, address)
	}
	return addresses
}

func GetAccountNames(keys keyring.Keyring) []string {
	recs, err := keys.List()
	if err != nil {
		panic(err)
	}
	names := make([]string, 0, len(recs))
	for _, rec := range recs {
		names = append(names, rec.Name)
	}
	return names
}

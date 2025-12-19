package testfactory

import (
	"bytes"
	"sort"

	"github.com/celestiaorg/celestia-app/v6/test/util/random"
	"github.com/celestiaorg/go-square/v3/share"
	"github.com/cosmos/cosmos-sdk/codec"
	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
)

const (
	// nolint:lll
	TestAccName  = "test-account"
	TestAccAddr  = "celestia1g39egf59z8tud3lcyjg5a83m20df4kccx32qkp"
	TestAccMnemo = `ramp soldier connect gadget domain mutual staff unusual first midnight iron good deputy wage vehicle mutual spike unlock rocket delay hundred script tumble choose`
	ChainID      = "test-app"
)

func Repeat[T any](s T, count int) []T {
	ss := make([]T, count)
	for i := range count {
		ss[i] = s
	}
	return ss
}

// GenerateRandNamespacedRawData returns random data of length count. Each chunk
// of random data is of size shareSize and is prefixed with a random blob
// namespace.
func GenerateRandNamespacedRawData(count int) (result [][]byte) {
	for range count {
		rawData := random.Bytes(share.ShareSize)
		namespace := share.RandomBlobNamespace().Bytes()
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
	for range count {
		accounts = append(accounts, random.Str(10))
	}
	return accounts
}

func GenerateAccounts(count int) []string {
	accounts := make([]string, count)
	for i := range count {
		// Generate a random private key
		privKey := secp256k1.GenPrivKey()
		// Get the public key and derive the address
		pubKey := privKey.PubKey()
		address := sdk.AccAddress(pubKey.Address())
		// Convert to bech32 string format
		accounts[i] = address.String()
	}
	return accounts
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

func TestKeyring(cdc codec.Codec, accounts ...string) keyring.Keyring {
	kb := keyring.NewInMemory(cdc)

	for _, acc := range accounts {
		_, _, err := kb.NewMnemonic(acc, keyring.English, "", "", hd.Secp256k1)
		if err != nil {
			panic(err)
		}
	}

	_, err := kb.NewAccount(TestAccName, TestAccMnemo, "", "", hd.Secp256k1)
	if err != nil {
		panic(err)
	}

	return kb
}

package types

import (
	"testing"

	"github.com/cosmos/cosmos-sdk/crypto/hd"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
)

const (
	// nolint:lll
	testMnemo   = `ramp soldier connect gadget domain mutual staff unusual first midnight iron good deputy wage vehicle mutual spike unlock rocket delay hundred script tumble choose`
	testAccName = "test-account"
	testChainID = "test-chain-1"
)

func generateKeyring(t *testing.T, accts ...string) keyring.Keyring {
	t.Helper()
	encCfg := makeBlobEncodingConfig()
	kb := keyring.NewInMemory(encCfg.Codec)

	for _, acc := range accts {
		_, _, err := kb.NewMnemonic(acc, keyring.English, "", "", hd.Secp256k1)
		if err != nil {
			t.Error(err)
		}
	}

	_, err := kb.NewAccount(testAccName, testMnemo, "1234", "", hd.Secp256k1)
	if err != nil {
		panic(err)
	}

	return kb
}

func generateKeyringSigner(t *testing.T, accts ...string) *KeyringSigner {
	kr := generateKeyring(t, accts...)
	return NewKeyringSigner(kr, testAccName, testChainID)
}

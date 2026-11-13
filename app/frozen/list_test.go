package frozen

import (
	"crypto/sha256"
	"encoding/hex"
	"testing"

	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

const (
	addr1 = "celestia1qyqszqgpqyqszqgpqyqszqgpqyqszqgpreswh3"
	addr2 = "celestia1qgpqyqszqgpqyqszqgpqyqszqgpqyqszjaktu8"
)

func TestAddresses(t *testing.T) {
	addrs, err := Addresses()
	require.NoError(t, err)
	require.Len(t, addrs, 41963)
	_, err = sdk.AccAddressFromBech32(Destination)
	require.NoError(t, err)
}

func TestParseList(t *testing.T) {
	addrs, err := ParseList("# comment\n\n " + addr1 + " \n" + addr2 + "\n")
	require.NoError(t, err)
	require.Len(t, addrs, 2)
	require.Equal(t, addr1, addrs[0].String())
	require.Equal(t, addr2, addrs[1].String())

	for name, list := range map[string]string{
		"duplicate":    addr1 + "\n" + addr2 + "\n" + addr1,
		"bad checksum": addr1[:len(addr1)-1] + "q",
		"wrong prefix": "cosmos1qyqszqgpqyqszqgpqyqszqgpqyqszqgpjnp7du",
		"destination":  Destination,
		"two per line": addr1 + " " + addr2,
	} {
		_, err := ParseList(list)
		require.Error(t, err, name)
	}
}

// TestConsensusInputsPinned fails on any change to the list or Destination.
// Both are frozen once v11.0.0 is tagged: changing them forks the upgrade.
func TestConsensusInputsPinned(t *testing.T) {
	sum := sha256.Sum256([]byte(addresses))
	require.Equal(t, "46c6222f1fb5b931ac103968965a9cb4ef26847bac984fd5a5e8f74304c1bba4", hex.EncodeToString(sum[:]))
	require.Equal(t, "celestia1atuap9kfnt6y8ykvn3437e7j3z8qffh33pzt53", Destination)
}

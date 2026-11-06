package app

import (
	"testing"

	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/stretchr/testify/require"
)

// TestModelableMsgTypeURLsResolve catches typos: every listed type must be a
// registered message.
func TestModelableMsgTypeURLsResolve(t *testing.T) {
	registry := encoding.MakeConfig(ModuleEncodingRegisters...).InterfaceRegistry
	for url := range modelableMsgTypeURLs {
		_, err := registry.Resolve(url)
		require.NoError(t, err, url)
	}
	_, err := registry.Resolve(msgRecvPacketTypeURL)
	require.NoError(t, err)
}

// TestICAHostDefaultsAreModelable checks that inbound IBC packets do not stop
// pay-for-fibre settlement under the default ICA host allowlist. Adding a type
// to IcaAllowMessages that is not modelable makes every block with an inbound
// packet stop settling pay-for-fibre txs.
func TestICAHostDefaultsAreModelable(t *testing.T) {
	for _, url := range IcaAllowMessages() {
		_, ok := modelableMsgTypeURLs[url]
		require.True(t, ok, url)
	}
}

package app

import (
	"testing"

	"github.com/stretchr/testify/require"
)

type mapAppOptions map[string]any

func (m mapAppOptions) Get(key string) any { return m[key] }

func TestSignaturePreverifyEnabled(t *testing.T) {
	require.True(t, signaturePreverifyEnabled(mapAppOptions{}))
	require.True(t, signaturePreverifyEnabled(mapAppOptions{FlagSignaturePreverify: true}))
	require.True(t, signaturePreverifyEnabled(mapAppOptions{FlagSignaturePreverify: "true"}))
	require.False(t, signaturePreverifyEnabled(mapAppOptions{FlagSignaturePreverify: false}))
	require.False(t, signaturePreverifyEnabled(mapAppOptions{FlagSignaturePreverify: "false"}))
	require.Panics(t, func() { signaturePreverifyEnabled(mapAppOptions{FlagSignaturePreverify: "nope"}) })
}

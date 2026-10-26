package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestResolveSSHPubKeyPath(t *testing.T) {
	t.Setenv(EnvVarSSHKeyPath, "/keys/id_ed25519")
	t.Setenv(EnvVarPubSSHKeyPath, "/keys/id_ed25519.pub")

	require.Equal(t, "/flag.pub", resolveSSHPubKeyPath("/flag.pub", "/config.pub"))
	require.Equal(t, "/keys/id_ed25519.pub", resolveSSHPubKeyPath("", "/config.pub"))

	t.Setenv(EnvVarPubSSHKeyPath, "")
	require.Equal(t, "/config.pub", resolveSSHPubKeyPath("", "/config.pub"))
}

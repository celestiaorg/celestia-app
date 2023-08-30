package types

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenerateKeyringSigner(t *testing.T) {
	account := "a"
	signer := GenerateKeyringSigner(t, account)
	got, err := signer.GetSignerInfo().GetAddress()

	require.NoError(t, err)
	assert.Contains(t, got.String(), "celestia")
}

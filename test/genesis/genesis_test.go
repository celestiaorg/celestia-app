package genesis

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGenesis(t *testing.T) {
	// create a default genesis file and check that all the fields are set
	g := NewDefaultGenesis().
		WithValidators(NewDefaultValidator("test-0")).
		WithAccounts(NewAccounts(1000, "test-1")...)

	require.Equal(t, 2, len(g.Accounts()))
	require.Equal(t, 1, len(g.Validators()))
	assert.NotEmpty(t, g.ChainID, "chain id not set")
	assert.False(t, g.GenesisTime.IsZero(), "genesis time not set")
	assert.NotNil(t, g.ConsensusParams, "consensus params not set")
	assert.NotNil(t, g.kr, "keyring not set")
	assert.NotNil(t, g.genOps, "genesis operations not set")
	assert.NotNil(t, g.ecfg.InterfaceRegistry, "encoding config not set")

	for _, acc := range g.Accounts() {
		assert.NotEmpty(t, acc.Name, "account name not set")
		assert.True(t, acc.InitialTokens > 0, "account initial tokens not set")
		assert.NotEmpty(t, acc.Mnemonic, "account mnemonic not set")
	}

}

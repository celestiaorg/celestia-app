package internal

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestGetGenesisVersion(t *testing.T) {
	t.Run("genesis.v3.json should return genesis version 1", func(t *testing.T) {
		genesisPath := "./testdata/genesis.v3.json"
		version, err := GetGenesisVersion(genesisPath)
		assert.NoError(t, err)
		assert.Equal(t, GenesisVersion1, version)
	})
	t.Run("genesis.v4.json should return genesis version 2", func(t *testing.T) {
		genesisPath := "./testdata/genesis.v4.json"
		version, err := GetGenesisVersion(genesisPath)
		assert.NoError(t, err)
		assert.Equal(t, GenesisVersion2, version)
	})
	t.Run("mocha.json should return genesis version 1", func(t *testing.T) {
		// mocha.json is a trimmed version of the Mocha genesis file which does
		// not contain messages or balances to reduce the file size.
		genesisPath := "./testdata/mocha.json"
		version, err := GetGenesisVersion(genesisPath)
		assert.NoError(t, err)
		assert.Equal(t, GenesisVersion1, version)
	})
}

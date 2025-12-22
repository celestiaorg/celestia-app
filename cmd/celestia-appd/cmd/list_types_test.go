package cmd

import (
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestListTypesCmd(t *testing.T) {
	cmd := listTypesCmd()

	// Test that the command can be created
	require.NotNil(t, cmd)
	assert.Equal(t, "list-types", cmd.Use)
	assert.Equal(t, "List all registered SDK messages, events, and proto types", cmd.Short)

	// Test that the command definition is correct
	assert.NotNil(t, cmd.RunE)
	assert.Contains(t, cmd.Long, "Lists all registered protobuf types")
	assert.Contains(t, cmd.Long, "interface registry")
}

package malicious

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/celestiaorg/celestia-app/v4/pkg/wrapper"
)

// TestNewConstructor verifies that the NewConstructor function works correctly
// and is compatible with the standard wrapper constructor interface.
func TestNewConstructor(t *testing.T) {
	squareSize := uint64(64)
	
	// Test that NewConstructor returns a valid TreeConstructorFn
	maliciousConstructor := NewConstructor(squareSize)
	require.NotNil(t, maliciousConstructor)
	
	// Test that it can create trees like the standard constructor
	goodConstructor := wrapper.NewConstructor(squareSize)
	require.NotNil(t, goodConstructor)
	
	// Both should be able to create trees
	maliciousTree := maliciousConstructor(0, 0)
	require.NotNil(t, maliciousTree)
	
	goodTree := goodConstructor(0, 0)
	require.NotNil(t, goodTree)
	
	// Both should implement the rsmt2d.Tree interface
	// We can test this by calling a method that should exist
	_, err := maliciousTree.Root()
	require.NoError(t, err)
	_, err = goodTree.Root()
	require.NoError(t, err)
}
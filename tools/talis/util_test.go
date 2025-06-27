package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMatchPattern(t *testing.T) {
	tests := []struct {
		name     string
		pattern  string
		input    string
		expected bool
	}{
		{"wildcard suffix match", "validator-*", "validator-0", true},
		{"wildcard matches anything", "*", "anything", true},
		{"wildcard matches exact", "node-*", "node-123", true},
		{"wildcard mismatch", "node-*", "validator-1", false},
		{"exact match", "node-1", "node-1", true},
		{"exact mismatch", "node-1", "node-2", false},
		{"prefix only", "*-0", "validator-0", true},
		{"suffix only", "validator-*", "node-0", false},
		{"empty pattern matches nothing", "", "anything", false},
		{"wildcard middle", "val*-1", "validator-1", true},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			match, err := matchPattern(tt.pattern, tt.input)
			require.NoError(t, err)
			require.Equal(t, tt.expected, match)
		})
	}
}

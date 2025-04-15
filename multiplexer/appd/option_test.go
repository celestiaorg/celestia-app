package appd

import (
	"bytes"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestCfgOptions(t *testing.T) {
	tests := []struct {
		name     string
		option   CfgOption
		expected func(*Appd) bool
	}{
		{
			name:   "WithStdOut sets stdout correctly",
			option: WithStdOut(&bytes.Buffer{}),
			expected: func(a *Appd) bool {
				_, ok := a.stdout.(*bytes.Buffer)
				return ok
			},
		},
		{
			name:   "WithStdErr sets stderr correctly",
			option: WithStdErr(&bytes.Buffer{}),
			expected: func(a *Appd) bool {
				_, ok := a.stderr.(*bytes.Buffer)
				return ok
			},
		},
		{
			name:   "WithStdIn sets stdin correctly",
			option: WithStdIn(&bytes.Buffer{}),
			expected: func(a *Appd) bool {
				_, ok := a.stdin.(*bytes.Buffer)
				return ok
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			appdInstance := &Appd{}

			// Apply the config option
			appdInstance = tt.option(appdInstance)

			require.NotNil(t, appdInstance, "Appd instance should not be nil")
			require.True(t, tt.expected(appdInstance), "Expected configuration was not applied")
		})
	}
}

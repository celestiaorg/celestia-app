package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestNodeNameWithChainAndExperiment(t *testing.T) {
	// Reset counters for clean testing
	valCount.Store(0)

	name1 := NodeNameWithChainAndExperiment(Validator, "talis-testchain", "exp1")
	require.Equal(t, "validator-0-talis-testchain-exp1", name1)

	name2 := NodeNameWithChainAndExperiment(Validator, "talis-testchain", "exp1")
	require.Equal(t, "validator-1-talis-testchain-exp1", name2)
}

func TestNewBaseInstanceWithChainAndExperiment(t *testing.T) {
	// Reset counters for clean testing
	valCount.Store(0)

	inst := NewBaseInstanceWithChainAndExperiment(Validator, "talis-testchain", "exp1")
	
	require.Equal(t, "validator-0-talis-testchain-exp1", inst.Name)
	require.Equal(t, Validator, inst.NodeType)
	require.Equal(t, "TBD", inst.PublicIP)
	require.Equal(t, "TBD", inst.PrivateIP)
	
	expectedTags := []string{"talis", "validator", "validator-0-talis-testchain-exp1", "talis-testchain", "exp1"}
	require.ElementsMatch(t, expectedTags, inst.Tags)
}

func TestWithDigitalOceanValidatorWithNewNaming(t *testing.T) {
	// Reset counters for clean testing  
	valCount.Store(0)

	cfg := NewConfig("test-experiment", "test-chain")
	
	// Add a validator and check that it uses the new naming pattern
	cfg = cfg.WithDigitalOceanValidator("nyc1")
	
	require.Len(t, cfg.Validators, 1)
	validator := cfg.Validators[0]
	
	// Should follow the pattern validator-index-chainID-experiment
	require.Equal(t, "validator-0-talis-test-chain-test-experiment", validator.Name)
	
	expectedTags := []string{"talis", "validator", "validator-0-talis-test-chain-test-experiment", "talis-test-chain", "test-experiment"}
	require.ElementsMatch(t, expectedTags, validator.Tags)
}

func TestWithDigitalOceanValidatorMultipleInstancesNewNaming(t *testing.T) {
	// Reset counters for clean testing
	valCount.Store(0)

	cfg := NewConfig("multi-experiment", "multi-chain")
	
	// Add multiple validators
	cfg = cfg.WithDigitalOceanValidator("nyc1")
	cfg = cfg.WithDigitalOceanValidator("sfo3")
	
	require.Len(t, cfg.Validators, 2)
	
	// Check that both validators follow the new naming pattern
	expectedNames := []string{
		"validator-0-talis-multi-chain-multi-experiment",
		"validator-1-talis-multi-chain-multi-experiment",
	}
	
	for i, validator := range cfg.Validators {
		require.Equal(t, expectedNames[i], validator.Name)
		expectedTags := []string{"talis", "validator", validator.Name, cfg.ChainID, cfg.Experiment}
		require.ElementsMatch(t, expectedTags, validator.Tags)
	}
}

func TestFilterMatchingInstances(t *testing.T) {
	// Reset counters for clean testing
	valCount.Store(0)

	// Create test instances with the new naming pattern
	instances := []Instance{
		NewBaseInstanceWithChainAndExperiment(Validator, "talis-testchain", "exp1"),
		NewBaseInstanceWithChainAndExperiment(Validator, "talis-testchain", "exp2"),
		NewBaseInstanceWithChainAndExperiment(Validator, "talis-prodchain", "exp1"),
	}

	tests := []struct {
		name     string
		pattern  string
		expected int
	}{
		{"all validators", "validator-*", 3},
		{"specific chain", "*-talis-testchain-*", 2},
		{"specific experiment", "*-exp1", 2},
		{"first validator", "validator-0-*", 1},
		{"specific validator", "validator-1-talis-testchain-exp2", 1},
		{"no matches", "*nonexistent*", 0},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			filtered, err := filterMatchingInstances(instances, tt.pattern)
			require.NoError(t, err)
			require.Len(t, filtered, tt.expected)
		})
	}
}
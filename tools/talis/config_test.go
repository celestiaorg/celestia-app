package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestWithDigitalOceanValidatorAddsTags(t *testing.T) {
	cfg := NewConfig("test-experiment", "test-chain")
	
	// Add a validator and check that it includes chain-id and experiment tags
	cfg = cfg.WithDigitalOceanValidator("nyc1")
	
	require.Len(t, cfg.Validators, 1)
	validator := cfg.Validators[0]
	
	expectedTags := []string{"talis", "validator", validator.Name, cfg.ChainID, cfg.Experiment}
	require.ElementsMatch(t, expectedTags, validator.Tags)
}

func TestWithDigitalOceanValidatorMultipleInstances(t *testing.T) {
	cfg := NewConfig("multi-experiment", "multi-chain")
	
	// Add multiple validators
	cfg = cfg.WithDigitalOceanValidator("nyc1")
	cfg = cfg.WithDigitalOceanValidator("sfo3")
	
	require.Len(t, cfg.Validators, 2)
	
	// Check that both validators have the correct tags
	for _, validator := range cfg.Validators {
		expectedTags := []string{"talis", "validator", validator.Name, cfg.ChainID, cfg.Experiment}
		require.ElementsMatch(t, expectedTags, validator.Tags)
	}
}

func TestConfigChainIDPrefixed(t *testing.T) {
	cfg := NewConfig("test-exp", "mychain")
	
	// Verify that chain ID is properly prefixed with "talis-"
	require.Equal(t, "talis-mychain", cfg.ChainID)
	
	cfg = cfg.WithDigitalOceanValidator("nyc1")
	validator := cfg.Validators[0]
	
	// The tag should include the prefixed chain ID
	require.Contains(t, validator.Tags, "talis-mychain")
}
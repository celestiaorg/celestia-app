package docker_e2e

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"

	celestiadockertypes "github.com/celestiaorg/tastora/framework/docker"
	"github.com/celestiaorg/tastora/framework/testutil/toml"
)

// embeddedAppOverrides configures the node for embedded application testing
// with KV indexing and WebAPI enabled
func embeddedAppOverrides() toml.Toml {
	overrides := make(toml.Toml)

	// Enable API server
	api := make(toml.Toml)
	api["enable"] = true
	api["enabled-unsafe-cors"] = true
	api["address"] = "tcp://0.0.0.0:1317"
	overrides["api"] = api

	// Enable gRPC
	grpc := make(toml.Toml)
	grpc["enable"] = true
	grpc["address"] = "0.0.0.0:9090"
	overrides["grpc"] = grpc

	// Enable gRPC Web
	grpcWeb := make(toml.Toml)
	grpcWeb["enable"] = true
	grpcWeb["address"] = "0.0.0.0:9091"
	overrides["grpc-web"] = grpcWeb

	return overrides
}

// embeddedAppConfigOverrides configures CometBFT for embedded application testing
// with KV indexing enabled
func embeddedAppConfigOverrides() toml.Toml {
	overrides := make(toml.Toml)

	// Enable transaction indexing
	txIndex := make(toml.Toml)
	txIndex["indexer"] = "kv"
	overrides["tx_index"] = txIndex

	// Enable RPC server
	rpc := make(toml.Toml)
	rpc["laddr"] = "tcp://0.0.0.0:26657"
	rpc["cors_allowed_origins"] = []string{"*"}
	overrides["rpc"] = rpc

	return overrides
}

func (s *CelestiaTestSuite) TestEmbeddedApplication() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := context.TODO()

	// Create a chain provider configured for embedded applications
	chainProvider := s.CreateDockerProvider(func(config *celestiadockertypes.Config) {
		numVals := 1
		numFullNodes := 0
		config.ChainConfig.NumValidators = &numVals
		config.ChainConfig.NumFullNodes = &numFullNodes

		// Configure overrides for embedded app testing
		config.ChainConfig.ConfigFileOverrides = map[string]any{
			"config/app.toml":    embeddedAppOverrides(),
			"config/config.toml": embeddedAppConfigOverrides(),
		}

		// Expose additional ports for API access
		config.ChainConfig.ExposeAdditionalPorts = []string{
			"1317/tcp", // REST API
			"9090/tcp", // gRPC
			"9091/tcp", // gRPC Web
		}

		// Add multiplexer-specific start args for embedded app simulation
		config.ChainConfig.AdditionalStartArgs = append(
			config.ChainConfig.AdditionalStartArgs,
			"--api.enable=true",
			"--api.address=tcp://0.0.0.0:1317",
			"--grpc.enable=true",
			"--grpc.address=0.0.0.0:9090",
		)

		config.ChainConfig.ModifyGenesis = func(c celestiadockertypes.Config, b []byte) ([]byte, error) {
			var doc GenesisHack
			if err := json.Unmarshal(b, &doc); err != nil {
				return nil, fmt.Errorf("failed to unmarshal genesis: %w", err)
			}

			doc.AppVersion = "3"
			doc.Consensus.Params.Block.TimeIotaMs = "1000"

			return json.MarshalIndent(doc, "", "  ")
		}
	})

	celestia, err := chainProvider.GetChain(ctx)
	s.Require().NoError(err, "failed to get chain")

	err = celestia.Start(ctx)
	s.Require().NoError(err, "failed to start chain")

	// Cleanup resources when the test is done
	t.Cleanup(func() {
		if err := celestia.Stop(ctx); err != nil {
			t.Logf("Error stopping chain: %v", err)
		}
	})

	// Verify the chain is running
	height, err := celestia.Height(ctx)
	s.Require().NoError(err, "failed to get chain height")
	s.Require().Greater(height, int64(0), "chain height is zero")

	t.Logf("Chain started successfully at height: %d", height)

	// Wait for the chain to produce some blocks
	s.Require().NoError(err, "failed to wait for blocks")
	time.Sleep(50 * time.Second)

}

// SetField modifies a JSON-encoded byte slice by updating or inserting a value at the given dot-delimited path.
// Returns the updated JSON-encoded byte slice or an error if unmarshalling, marshaling, or setting the field fails.
func SetField(bz []byte, path string, value interface{}) ([]byte, error) {
	var doc map[string]interface{}
	if err := json.Unmarshal(bz, &doc); err != nil {
		return nil, fmt.Errorf("failed to unmarshal genesis: %w", err)
	}

	if err := setOrDeleteNestedField(doc, path, value); err != nil {
		return nil, err
	}

	return json.MarshalIndent(doc, "", "  ")
}

// RemoveField removes a JSON field identified by the path from the provided byte slice and returns the updated JSON or an error.
func RemoveField(bz []byte, path string) ([]byte, error) {
	return SetField(bz, path, nil)
}

// setOrDeleteNestedField modifies a nested field in a map based on a dot-delimited path or deletes it if value is nil.
// Returns an error if the path is invalid or intermediate nodes are not maps.
func setOrDeleteNestedField(doc map[string]interface{}, path string, value interface{}) error {
	keys := strings.Split(path, ".")

	current := doc
	for i, key := range keys {
		// if it's the last key, set the value
		if i == len(keys)-1 {
			if value == nil {
				delete(current, key)
				return nil
			}
			current[key] = value
			return nil
		}

		next, ok := current[key].(map[string]interface{})
		if !ok {
			return fmt.Errorf("invalid path: %s is not a map", strings.Join(keys[:i+1], "."))
		}
		current = next
	}
	return nil
}

type GenesisHack struct {
	AppName       string          `json:"app_name"`
	AppVersion    string          `json:"app_version"`
	GenesisTime   time.Time       `json:"genesis_time"`
	ChainID       string          `json:"chain_id"`
	InitialHeight int             `json:"initial_height"`
	AppHash       interface{}     `json:"app_hash"`
	AppState      json.RawMessage `json:"app_state"`
	Consensus     struct {
		Params struct {
			Block struct {
				MaxBytes   string `json:"max_bytes"`
				MaxGas     string `json:"max_gas"`
				TimeIotaMs string `json:"time_iota_ms,omitempty"`
			} `json:"block"`
			Evidence struct {
				MaxAgeNumBlocks string `json:"max_age_num_blocks"`
				MaxAgeDuration  string `json:"max_age_duration"`
				MaxBytes        string `json:"max_bytes"`
			} `json:"evidence"`
			Validator struct {
				PubKeyTypes []string `json:"pub_key_types"`
			} `json:"validator"`
			Version struct {
				App string `json:"app"`
			} `json:"version"`
			Abci struct {
				VoteExtensionsEnableHeight string `json:"vote_extensions_enable_height"`
			} `json:"abci"`
		} `json:"params"`
	} `json:"consensus"`
}

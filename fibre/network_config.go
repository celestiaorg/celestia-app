package fibre

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"os"

	fibregrpc "github.com/celestiaorg/celestia-app/v10/fibre/internal/grpc"
)

// NetworkConfig maps validator identities to source-bound TCP paths.
type NetworkConfig = fibregrpc.NetworkConfig

// LoadNetworkConfig reads and validates a network routing file of at most 1 MiB.
func LoadNetworkConfig(path string) (*NetworkConfig, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	const maxSize = 1 << 20
	data, err := io.ReadAll(io.LimitReader(f, maxSize+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxSize {
		return nil, fmt.Errorf("network config exceeds 1 MiB")
	}
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	var cfg NetworkConfig
	if err := decoder.Decode(&cfg); err != nil {
		return nil, err
	}
	if err := decoder.Decode(new(any)); err != io.EOF {
		return nil, fmt.Errorf("network config must contain one JSON object")
	}
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	return &cfg, nil
}

package e2e

import (
	"fmt"
	"os"

	"github.com/BurntSushi/toml"
)

type Manifest struct {
	Nodes    map[string]*ManifestNode    `toml:"node"`
	Accounts map[string]*ManifestAccount `toml:"account"`
}

type ManifestNode struct {
	// Versions is an array of binary versions that the node
	// will run in it's lifetime. A series of upgrades can be
	// triggered by the upgrade command or will happen automatically
	// across a testnet.
	//
	// Version strings are used to pull  docker images from ghcr.io.
	// Alternatively, use "current", if you want to use the binary
	// based on the current branch. You must have the image built by
	// running "make docker". Default set to "current"
	Versions []string `toml:"versions"`

	// The height that the node will start at
	StartHeight int64 `toml:"start_height"`

	// Peers are the set of peers that initially populate the address
	// book. Persistent peers and seeds are currently not supported
	// By default all nodes declared in the manifest are included
	Peers []string `toml:"peers"`

	// SelfDelegation is the delegation of the validator when they
	// first come up. If set to 0, the node is not considered a validator
	SelfDelegation int64 `toml:"self_delegation"`
}

// ManifestAccounts represent SDK accounts that sign and
// submit transactions. If the account has the same name as
// the node it is the operator address for the validator.
// Unless specified it will have a default self delegation
// All accounts specfied are created at genesis.
type ManifestAccount struct {
	// Tokens symbolizes the genesis supply of liquid tokens to that account
	Tokens int64 `toml:"tokens"`

	// The key type to derive the account key from. Defaults to secp256k1
	KeyType string `toml:"key_type"`
}

// Save saves the testnet manifest to a file.
func (m Manifest) Save(file string) error {
	f, err := os.Create(file)
	if err != nil {
		return fmt.Errorf("failed to create manifest file %q: %w", file, err)
	}
	return toml.NewEncoder(f).Encode(m)
}

// LoadManifest loads a testnet manifest from a file.
func LoadManifest(file string) (Manifest, error) {
	manifest := Manifest{}
	_, err := toml.DecodeFile(file, &manifest)
	if err != nil {
		return manifest, fmt.Errorf("failed to load testnet manifest %q: %w", file, err)
	}
	return manifest, nil
}

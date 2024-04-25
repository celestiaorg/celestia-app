package testnets

import (
	"time"
)

// TestManifest defines the parameters for a testnet.
type TestManifest struct {
	ChainID string
	// Number of validators in the testnet
	Validators int
	// Resource requirements for a validator node
	ValidatorResource Resources
	// Resource requirements for a tx client
	TxClientsResource Resources
	// Self-delegation amount for validators
	SelfDelegation int64
	// CelestiaAppVersion a specific version of the celestia-app container image within celestiaorg repository on GitHub's Container Registry i.e., https://github.com/celestiaorg/celestia-app/pkgs/container/celestia-app
	CelestiaAppVersion string
	// TxClientVersion  a specific version of the txsim container image within celestiaorg repository on GitHub's Container Registry, i.e., https://github.com/celestiaorg/celestia-app/pkgs/container/txsim
	TxClientVersion string

	// tx client settings
	// Number of blobs per sequence
	BlobsPerSeq int
	// Number of blob sequences
	BlobSequences int
	// Size of blobs in bytes, e.g., "10000" (exact size) or "10000-20000" (min-max format)
	BlobSizes string

	// p2p configs
	// Bandwidth per peer in bytes per second
	PerPeerBandwidth int64
	// consensus configs
	// if TimeoutCommit is set to 0, it won't take effect and a default value will be used
	TimeoutCommit time.Duration
	// if TimeoutPropose is set to 0, it won't take effect and a default value will be used
	TimeoutPropose time.Duration

	// Mempool configs
	// Mempool version
	// If Mempool is set to "", it won't take effect and a default value will be used
	Mempool      string
	BroadcastTxs bool

	// prometheus configs
	Prometheus bool

	// consensus manifest
	// If MaxBlockBytes is set to 0, it won't take effect and a default value will be used
	MaxBlockBytes int64

	// other configs
	UpgradeHeight int64 // Upgrade height
	// if GovMaxSquareSize is set to 0, it won't take effect and a default value will be used
	GovMaxSquareSize int64

	TestDuration time.Duration
	TxClientsNum int
}

package testnets

import (
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/tendermint/tendermint/config"
)

// TestnetSetting defines the parameters for a testnet.
type TestnetSetting struct {
	ChainID           string
	Validators        int       // Number of validators in the testnet
	ValidatorResource Resources // Resource requirements for a validator node
	SelfDelegation    int64     // Self-delegation amount for validators
	Version           string    // Version of the app to use for validators
	// tx client settings
	BlobsPerSeq   int // Number of blobs per sequence
	BlobSequences int // Number of blob sequences
	BlobSizes     int // Size of blobs in bytes
	// p2p configs
	PerPeerBandwidth int64 // Bandwidth per peer in bytes per second
	// consensus configs
	TimeoutCommit  time.Duration
	TimeoutPropose time.Duration
	// mempool configs
	Mempool      string // Mempool version
	BroadcastTxs bool
	// prometheus configs
	Prometheus bool
	// consensus params
	MaxBlockBytes int64
	// other configs
	UpgradeHeight    int64 // Upgrade height
	GovMaxSquareSize int64
}

func GetTestnetDefaultSetting() TestnetSetting {
	cfg := config.DefaultConfig()
	appParams := app.DefaultInitialConsensusParams()
	var defaultParams = TestnetSetting{
		ChainID:           "test-chain",
		Validators:        4,
		ValidatorResource: DefaultResources,
		SelfDelegation:    10000000,
		Version:           "latest",
		BlobsPerSeq:       1,
		BlobSequences:     1,
		BlobSizes:         10 * 1024,
		PerPeerBandwidth:  cfg.P2P.SendRate,
		UpgradeHeight:     0,
		TimeoutCommit:     cfg.Consensus.TimeoutCommit,
		TimeoutPropose:    cfg.Consensus.TimeoutPropose,
		Mempool:           cfg.Mempool.Version,
		BroadcastTxs:      cfg.Mempool.Broadcast,
		Prometheus:        cfg.Instrumentation.Prometheus,
		GovMaxSquareSize:  appconsts.DefaultGovMaxSquareSize,
		MaxBlockBytes:     appParams.Block.MaxBytes,
	}
	return defaultParams
}

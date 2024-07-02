package main

import (
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/app/encoding"
	"github.com/celestiaorg/celestia-app/v2/test/e2e/testnet"
	"github.com/celestiaorg/celestia-app/v2/test/util/genesis"
	blobtypes "github.com/celestiaorg/celestia-app/v2/x/blob/types"
	tmproto "github.com/tendermint/tendermint/proto/tendermint/types"
)

type LatencyParams struct {
	// Latency in milliseconds
	Latency int64
	// Jitter in milliseconds
	Jitter int64
}

// Manifest defines the parameters for a testnet.
type Manifest struct {
	TestnetName  string
	ChainID      string
	TestDuration time.Duration
	// Number of validators in the testnet
	Validators int
	// Number of tx clients (txsim for now) in the testnet; there will be 1 txclient per validator
	// if TXClients is less than Validators, the remaining validators will not have any txclients
	TxClients int
	// Self-delegation amount for validators
	SelfDelegation int64
	// CelestiaAppVersion a specific version of the celestia-app container image within celestiaorg repository on GitHub's Container Registry i.e., https://github.com/celestiaorg/celestia-app/pkgs/container/celestia-app
	CelestiaAppVersion string
	// TxClientVersion  a specific version of the txsim container image within celestiaorg repository on GitHub's Container Registry, i.e., https://github.com/celestiaorg/celestia-app/pkgs/container/txsim
	TxClientVersion string
	// Resource requirements for a validator node
	ValidatorResource testnet.Resources
	// Resource requirements for a tx client
	TxClientsResource testnet.Resources
	// EnableLatency enables network latency for the validators
	EnableLatency bool

	// LatencyParams defines the network latency parameters
	LatencyParams LatencyParams

	// BandwidthParams defines the network bandwidth parameters
	BandwidthParams int64 // in bps e.g. 1000000 = 1Mbps

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
	TimeoutCommit  time.Duration
	TimeoutPropose time.Duration

	// Mempool configs
	// Mempool version
	Mempool      string
	BroadcastTxs bool

	// prometheus configs
	Prometheus bool

	// consensus parameters
	MaxBlockBytes int64

	// LocalTracingType can be "local" or "noop"
	LocalTracingType string
	PushTrace        bool
	// download traces from the s3 bucket
	// only available when PushTrace is enabled
	DownloadTraces bool

	UpgradeHeight    int64
	GovMaxSquareSize int64
}

func (m *Manifest) GetGenesisModifiers() []genesis.Modifier {
	ecfg := encoding.MakeConfig(app.ModuleBasics)
	var modifiers []genesis.Modifier

	blobParams := blobtypes.DefaultParams()
	blobParams.GovMaxSquareSize = uint64(m.GovMaxSquareSize)
	modifiers = append(modifiers, genesis.SetBlobParams(ecfg.Codec, blobParams))

	return modifiers
}

func (m *Manifest) GetConsensusParams() *tmproto.ConsensusParams {
	cparams := app.DefaultConsensusParams()
	cparams.Block.MaxBytes = m.MaxBlockBytes
	return cparams
}

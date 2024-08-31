package main

import (
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/v3/app"
	"github.com/celestiaorg/celestia-app/v3/app/encoding"
	"github.com/celestiaorg/celestia-app/v3/test/e2e/testnet"
	"github.com/celestiaorg/celestia-app/v3/test/util/genesis"
	blobtypes "github.com/celestiaorg/celestia-app/v3/x/blob/types"
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

	DisableBBR bool
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

// summary generates a summary of the Manifest struct to be used as chain id.
func (m *Manifest) summary() string {
	latency := 0
	if m.EnableLatency {
		latency = 1
	}
	fmt.Println("latency is", latency)
	bbr := 1
	if m.DisableBBR {
		bbr = 0
	}
	maxBlockMB := m.MaxBlockBytes / testnet.MB
	summary := fmt.Sprintf("v%d-t%d-b%d-bw%dmb-tc%d-tp%d-l%d-%s-br%d-%dmb",
		m.Validators, m.TxClients,
		m.BlobSequences, m.PerPeerBandwidth/testnet.MB,
		m.TimeoutCommit/time.Second, m.TimeoutPropose/time.Second,
		latency, m.Mempool, bbr, maxBlockMB)
	if len(summary) > 50 {
		return summary[:50]
	}
	return summary
}

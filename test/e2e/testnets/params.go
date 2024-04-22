package testnets

import (
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	"github.com/celestiaorg/celestia-app/v2/pkg/appconsts"
	"github.com/tendermint/tendermint/config"
)

type TestnetParams struct {
	ChainID          string
	Validators       int
	FullNodes        int
	BlobsPerSeq      int
	BlobSequences    int
	BlobSizes        int
	PerPeerBandwidth int64
	TimeoutCommit    time.Duration
	TimeoutPropose   time.Duration
	Mempool          string
	BroadcastTxs     bool
	Prometheus       bool

	GovMaxSquareSize int
	MaxBlockBytes    int64
}

func GetTestDefaultParams() TestnetParams {
	cfg := config.DefaultConfig()
	appParams := app.DefaultInitialConsensusParams()
	var defaultParams = TestnetParams{
		ChainID:       "test-chain",
		Validators:    4,
		FullNodes:     1,
		BlobsPerSeq:   1,
		BlobSequences: 1,
		BlobSizes:     10 * 1024,
		//GovMaxSquareSize:
		PerPeerBandwidth: cfg.P2P.SendRate,
		TimeoutCommit:    cfg.Consensus.TimeoutCommit,
		TimeoutPropose:   cfg.Consensus.TimeoutPropose,
		Mempool:          cfg.Mempool.Version,
		BroadcastTxs:     cfg.Mempool.Broadcast,
		Prometheus:       cfg.Instrumentation.Prometheus,
		GovMaxSquareSize: appconsts.DefaultGovMaxSquareSize,
		MaxBlockBytes:    appParams.Block.MaxBytes,
	}
	return defaultParams
}

package testnet

import (
	"fmt"
	"strings"
	"time"

	"github.com/cometbft/cometbft/config"
	"github.com/cometbft/cometbft/p2p"
	"github.com/cometbft/cometbft/p2p/pex"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"

	"github.com/celestiaorg/celestia-app/v4/app"
	"github.com/celestiaorg/celestia-app/v4/app/params"
)

func MakeConfig(node *Node, peers []string, opts ...Option) (*config.Config, error) {
	cfg := app.DefaultConsensusConfig()
	cfg.TxIndex.Indexer = "kv"
	cfg.Consensus.TimeoutPropose = config.DefaultConsensusConfig().TimeoutPropose
	cfg.Consensus.TimeoutCommit = config.DefaultConsensusConfig().TimeoutCommit
	cfg.Moniker = node.Name
	cfg.RPC.ListenAddress = "tcp://0.0.0.0:26657"
	cfg.P2P.PersistentPeers = strings.Join(peers, ",")
	cfg.Instrumentation.Prometheus = true

	for _, opt := range opts {
		opt(cfg)
	}

	return cfg, nil
}

type Option func(*config.Config)

func WithPerPeerBandwidth(bandwidth int64) Option {
	return func(cfg *config.Config) {
		cfg.P2P.SendRate = bandwidth
		cfg.P2P.RecvRate = bandwidth
	}
}

func WithTimeoutPropose(timeout time.Duration) Option {
	return func(cfg *config.Config) {
		cfg.Consensus.TimeoutPropose = timeout
	}
}

func WithTimeoutCommit(timeout time.Duration) Option {
	return func(cfg *config.Config) {
		cfg.Consensus.TimeoutCommit = timeout
	}
}

func WithPrometheus(prometheus bool) Option {
	return func(cfg *config.Config) {
		cfg.Instrumentation.Prometheus = prometheus
	}
}

func WithMempool(mempool string) Option {
	return func(cfg *config.Config) {
		cfg.Mempool.Type = mempool
	}
}

func WithBroadcastTxs(broadcast bool) Option {
	return func(cfg *config.Config) {
		cfg.Mempool.Broadcast = broadcast
	}
}

func WithLocalTracing(localTracingType string) Option {
	return func(cfg *config.Config) {
		cfg.Instrumentation.TraceType = localTracingType
		cfg.Instrumentation.TraceBufferSize = 1000
		cfg.Instrumentation.TracePullAddress = ":26661"
	}
}

func WithTxIndexer(indexer string) Option {
	return func(cfg *config.Config) {
		cfg.TxIndex.Indexer = indexer
	}
}

func WithMempoolMaxTxsBytes(maxTxsBytes int64) Option {
	return func(cfg *config.Config) {
		cfg.Mempool.MaxTxsBytes = maxTxsBytes
	}
}

func WithMempoolMaxTxBytes(maxTxBytes int) Option {
	return func(cfg *config.Config) {
		cfg.Mempool.MaxTxBytes = maxTxBytes
	}
}

func WriteAddressBook(peers []string, file string) error {
	book := pex.NewAddrBook(file, false)
	for _, peer := range peers {
		addr, err := p2p.NewNetAddressString(peer)
		if err != nil {
			return fmt.Errorf("parsing peer address %s: %w", peer, err)
		}
		err = book.AddAddress(addr, addr)
		if err != nil {
			return fmt.Errorf("adding peer address %s: %w", peer, err)
		}
	}
	book.Save()
	return nil
}

func MakeAppConfig(_ *Node) (*serverconfig.Config, error) {
	srvCfg := serverconfig.DefaultConfig()
	srvCfg.MinGasPrices = fmt.Sprintf("0.001%s", params.BondDenom)
	// updating MaxRecvMsgSize and MaxSendMsgSize allows submission of 128MiB worth of
	// transactions simultaneously which is useful for big block tests.
	srvCfg.GRPC.MaxRecvMsgSize = 128 * MiB
	srvCfg.GRPC.MaxSendMsgSize = 128 * MiB
	srvCfg.GRPC.Enable = true
	srvCfg.GRPC.Address = "0.0.0.0:9090" // explicitly ensure that other containers can access the address.

	if err := validateServerConfigForTestnet(srvCfg); err != nil {
		return nil, err
	}

	return srvCfg, srvCfg.ValidateBasic()
}

// validateServerConfigForTestnet ensures that the server config is configured correctly
// to ensure nodes are accessible during tests.
func validateServerConfigForTestnet(srvCfg *serverconfig.Config) error {
	if !srvCfg.GRPC.Enable {
		return fmt.Errorf("gRPC must be enabled")
	}

	if !strings.Contains(srvCfg.GRPC.Address, "0.0.0.0") {
		return fmt.Errorf("gRPC address must contain '0.0.0.0'")
	}
	return nil
}

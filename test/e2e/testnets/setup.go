package testnets

import (
	"fmt"
	"strings"
	"time"

	"github.com/celestiaorg/celestia-app/v2/app"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/p2p/pex"
)

func MakeConfig(node *Node, opt ...option) (*config.Config, error) {
	cfg := config.DefaultConfig()
	cfg.Moniker = node.Name
	cfg.RPC.ListenAddress = "tcp://0.0.0.0:26657"
	cfg.P2P.ExternalAddress = fmt.Sprintf("tcp://%v", node.AddressP2P(false))
	cfg.P2P.PersistentPeers = strings.Join(node.InitialPeers, ",")
	for _, o := range opt {
		cfg = o(cfg)

	}
	return cfg, nil
}

type option func(*config.Config) *config.Config

func WithPerPeerBandwidth(bandwidth int64) option {
	return func(cfg *config.Config) *config.Config {
		cfg.P2P.SendRate = bandwidth
		cfg.P2P.RecvRate = bandwidth
		return cfg
	}
}

func WithTimeoutPropose(timeout time.Duration) option {
	return func(cfg *config.Config) *config.Config {
		cfg.Consensus.TimeoutPropose = timeout
		return cfg
	}
}

func WithTimeoutCommit(timeout time.Duration) option {
	return func(cfg *config.Config) *config.Config {
		cfg.Consensus.TimeoutCommit = timeout
		return cfg
	}
}

func WithPrometheus(prometheus bool) option {
	return func(cfg *config.Config) *config.Config {
		cfg.Instrumentation.Prometheus = prometheus
		return cfg
	}
}

func WithMempool(mempool string) option {
	return func(cfg *config.Config) *config.Config {
		cfg.Mempool.Version = mempool
		return cfg
	}
}

func WithBroadcastTxs(broadcast bool) option {
	return func(cfg *config.Config) *config.Config {
		cfg.Mempool.Broadcast = broadcast
		return cfg
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
	srvCfg.MinGasPrices = fmt.Sprintf("0.001%s", app.BondDenom)
	return srvCfg, srvCfg.ValidateBasic()
}

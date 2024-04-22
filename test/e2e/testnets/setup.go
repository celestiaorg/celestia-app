package testnets

import (
	"fmt"
	"strings"

	"github.com/celestiaorg/celestia-app/v2/app"
	serverconfig "github.com/cosmos/cosmos-sdk/server/config"
	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/p2p/pex"
)

func MakeConfig(testnet Testnet, node *Node) (*config.Config, error) {
	cfg := config.DefaultConfig()
	cfg.Moniker = node.Name
	cfg.RPC.ListenAddress = "tcp://0.0.0.0:26657"
	cfg.P2P.ExternalAddress = fmt.Sprintf("tcp://%v", node.AddressP2P(false))
	cfg.P2P.PersistentPeers = strings.Join(node.InitialPeers, ",")
	cfg.P2P.SendRate = testnet.params.PerPeerBandwidth           // 5 * 1024 * 1024 // 5MiB/s
	cfg.P2P.RecvRate = testnet.params.PerPeerBandwidth           // 5 * 1024 * 1024 // 5MiB/s
	cfg.Consensus.TimeoutPropose = testnet.params.TimeoutPropose //1 * time. Second
	cfg.Consensus.TimeoutCommit = testnet.params.TimeoutCommit   //1 * time.Second
	cfg.Instrumentation.Prometheus = testnet.params.Prometheus
	cfg.Mempool.Broadcast = testnet.params.BroadcastTxs
	cfg.Mempool.Version = testnet.params.Mempool
	return cfg, nil
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

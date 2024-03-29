package sanity

import (
	"fmt"

	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/crypto/ed25519"
	cmtnet "github.com/tendermint/tendermint/libs/net"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/p2p/conn"
	"github.com/tendermint/tendermint/version"
)

var defaultProtocolVersion = p2p.NewProtocolVersion(
	version.P2PProtocol,
	version.BlockProtocol,
	0,
)

type Node struct {
	key ed25519.PrivKey
	id  p2p.ID
	// cfg    peerConfig
	p2pCfg config.P2PConfig
	addr   *p2p.NetAddress
	sw     *p2p.Switch
	mt     *p2p.MultiplexTransport
}

// newNode creates a new local peer with a random key.
func NewNode(p2pCfg config.P2PConfig, mcfg conn.MConnConfig, rs ...p2p.Reactor) (*Node, error) {
	port, err := cmtnet.GetFreePort()
	if err != nil {
		return nil, err
	}
	p2pCfg.ListenAddress = fmt.Sprintf("tcp://localhost:%d", port)
	key := ed25519.GenPrivKey()
	n := &Node{
		key: key,
		id:  p2p.PubKeyToID(key.PubKey()),
		// cfg:    cfg,
		p2pCfg: p2pCfg,
	}
	addr, err := p2p.NewNetAddressString(p2p.IDAddressString(n.id, p2pCfg.ListenAddress))
	if err != nil {
		return nil, err
	}
	n.addr = addr

	channelIDs := make([]byte, 0)
	for _, r := range rs {
		ch := r.GetChannels()
		for _, c := range ch {
			channelIDs = append(channelIDs, c.ID)
		}
	}

	NodeInfo := p2p.DefaultNodeInfo{
		ProtocolVersion: defaultProtocolVersion,
		ListenAddr:      p2pCfg.ListenAddress,
		DefaultNodeID:   n.id,
		Network:         "test",
		Version:         "1.2.3-rc0-deadbeef",
		Moniker:         "test",
		Channels:        channelIDs,
	}

	mt := p2p.NewMultiplexTransport(
		NodeInfo,
		p2p.NodeKey{PrivKey: key},
		mcfg,
	)

	n.mt = mt

	sw := newSwitch(p2pCfg, mt, rs...)
	n.sw = sw
	return n, nil
}

func (n *Node) start() error {
	err := n.mt.Listen(*n.addr)
	if err != nil {
		return err
	}

	if err := n.sw.Start(); err != nil {
		return err
	}
	return nil
}

func (n *Node) stop() {
	_ = n.sw.Stop()
	_ = n.mt.Close()
}

func newSwitch(cfg config.P2PConfig, mt *p2p.MultiplexTransport, rs ...p2p.Reactor) *p2p.Switch {
	sw := p2p.NewSwitch(&cfg, mt)
	for i, r := range rs {
		sw.AddReactor(fmt.Sprintf("reactor%d", i), r)
	}
	return sw
}

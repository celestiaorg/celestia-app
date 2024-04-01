package sanity

import (
	"fmt"
	"net"
	"strconv"
	"strings"

	"github.com/tendermint/tendermint/config"
	"github.com/tendermint/tendermint/crypto/ed25519"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/p2p/conn"
	"github.com/tendermint/tendermint/version"
)

var defaultProtocolVersion = p2p.NewProtocolVersion(
	version.P2PProtocol,
	version.BlockProtocol,
	0,
)

const (
	Port = 26656
)

type Node struct {
	key ed25519.PrivKey
	ip  net.IP
	id  p2p.ID
	// cfg    peerConfig
	p2pCfg config.P2PConfig
	addr   *p2p.NetAddress
	sw     *p2p.Switch
	mt     *p2p.MultiplexTransport
}

// newNode creates a new local peer with a random key.
func NewNode(p2pCfg config.P2PConfig, mcfg conn.MConnConfig, ip net.IP, rs ...p2p.Reactor) (*Node, error) {
	p2pCfg.ListenAddress = fmt.Sprintf("tcp://0.0.0.0:%d", Port)
	key := ed25519.GenPrivKey()
	n := &Node{
		key:    key,
		id:     p2p.PubKeyToID(key.PubKey()),
		ip:     ip,
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

func (n *Node) Key() ed25519.PrivKey {
	return n.key
}

func (n *Node) PeerAddress() string {
	return peerID(n.ip, n.key)
}

func peerID(ip net.IP, networkKey ed25519.PrivKey) string {
	nodeID := string(p2p.PubKeyToID(networkKey.PubKey()))
	return fmt.Sprintf("%s@%s:%d", nodeID, ip, Port)
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

// parsePeerID takes a string in the format "nodeID@ip:port" and returns the nodeID, ip, and port.
func parsePeerID(peerID string) (nodeID string, ip net.IP, port int, err error) {
	// Split the string by '@' to separate nodeID and the rest.
	atSplit := strings.SplitN(peerID, "@", 2)
	if len(atSplit) != 2 {
		err = fmt.Errorf("invalid format, missing '@'")
		return
	}
	nodeID = atSplit[0]

	// Split the second part by ':' to separate IP and port.
	colonSplit := strings.SplitN(atSplit[1], ":", 2)
	if len(colonSplit) != 2 {
		err = fmt.Errorf("invalid format, missing ':'")
		return
	}

	ip = net.ParseIP(colonSplit[0])
	if ip == nil {
		err = fmt.Errorf("invalid IP address")
		return
	}

	port, err = strconv.Atoi(colonSplit[1])
	if err != nil {
		err = fmt.Errorf("invalid port: %w", err)
		return
	}

	return
}

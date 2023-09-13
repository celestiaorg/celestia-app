package network

import (
	"errors"
	"fmt"
	"strings"

	"github.com/testground/sdk-go/runtime"
)

const (
	TopologyParamKey            = "topology"
	ConnectAllTopology          = "connect_all"
	ConnectSubsetTopology       = "connect_subset"
	PersistentPeerCountParamKey = "persistent-peer-count"
)

func DefaultTopologies() []string {
	return []string{
		ConnectAllTopology,
	}
}

func GetTopologyFns(runenv *runtime.RunEnv) ([]TopologyFn, error) {
	topology := runenv.StringParam(TopologyParamKey)
	if topology == "" {
		topology = ConnectAllTopology
	}
	tops := make([]TopologyFn, 0)
	// TODO: fix the toml parser so that it can handle string arrays
	for _, topology := range []string{topology} {
		switch topology {
		case ConnectAllTopology:
			tops = append(tops, ConnectAll)
		case ConnectSubsetTopology:
			numPeers := runenv.IntParam(PersistentPeerCountParamKey)
			tops = append(tops, ConnectSubset(numPeers))
		default:
			return nil, fmt.Errorf("unknown topology func: %s", topology)
		}
	}
	return tops, nil
}

// TopologyFn is a function that arbitarily modifies the provided node
// configurations. It is used to generate the topology (which nodes are
// connected to which) of the network, along with making other arbitrary changes
// to the configs.
type TopologyFn func(nodes []NodeConfig) ([]NodeConfig, error)

var _ = TopologyFn(ConnectAll)
var _ = TopologyFn(ConnectSubset(10))

// ConnectAll is a TopologyFn that connects all nodes to each other via
// persistent peers.
func ConnectAll(nodes []NodeConfig) ([]NodeConfig, error) {
	peerIDs := make([]string, 0, len(nodes))
	for _, nodeCfg := range nodes {
		peerIDs = append(peerIDs, nodeCfg.P2PID)
	}

	// For each node, generate the string that excludes its own P2PID
	for nodeID, nodeConfig := range nodes {
		var filteredP2PIDs []string
		for _, pid := range peerIDs {
			if pid != nodeConfig.P2PID {
				filteredP2PIDs = append(filteredP2PIDs, pid)
			}
		}

		// Here you could put the concatenated string into another field in NodeConfig
		// or do whatever you want with it.
		nodeConfig.CmtConfig.P2P.PersistentPeers = strings.Join(filteredP2PIDs, ",")
		nodes[nodeID] = nodeConfig
	}

	return nodes, nil
}

// ConnectSubset is a TopologyFn that connects each node to a subset of other
// nodes via persistent peers. The subset is rotated for each node to minimize
// overlap.
func ConnectSubset(numPeers int) TopologyFn {
	return func(nodes []NodeConfig) ([]NodeConfig, error) {
		if len(nodes) < 1 {
			return nil, errors.New("no nodes to generate topology for")
		}

		if numPeers >= len(nodes) {
			return nil, errors.New("number of peers to connect should be less than total number of nodes")
		}

		peerIDs := make([]string, 0, len(nodes))
		for _, nodeCfg := range nodes {
			peerIDs = append(peerIDs, nodeCfg.P2PID)
		}

		// For each node, generate the list of peers that minimizes overlap
		for i, nodeConfig := range nodes {
			var filteredP2PIDs []string

			// Locate the index of this node in the peerIDs array
			var startIndex int
			for i, pid := range peerIDs {
				if pid == nodeConfig.P2PID {
					startIndex = i
					break
				}
			}

			// Collect numPeers number of P2P IDs, skipping peers to minimize overlap
			skip := len(peerIDs) / (numPeers + 1) // Number of peers to skip to get next peer
			for i := 1; i <= numPeers; i++ {
				targetIndex := (startIndex + i*skip) % len(peerIDs)
				filteredP2PIDs = append(filteredP2PIDs, peerIDs[targetIndex])
			}

			// Put the concatenated string into the appropriate field in NodeConfig.
			// Here I assume there is a CmtConfig field and a PersistentPeers field within it.
			nodes[i].CmtConfig.P2P.PersistentPeers = strings.Join(filteredP2PIDs, ",")
		}

		return nodes, nil
	}
}

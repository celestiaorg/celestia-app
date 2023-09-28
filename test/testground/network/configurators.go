package network

import (
	"fmt"
	"sort"
	"strings"

	"github.com/testground/sdk-go/runtime"
)

const (
	TopologyParam      = "topology"
	ConnectAllTopology = "connect_all"
	SeedTopology       = "seed"
	SeedGroupID        = "seed"
)

func DefaultTopologies() []string {
	return []string{
		ConnectAllTopology,
	}
}

// GetConfigurators
func GetConfigurators(runenv *runtime.RunEnv) ([]Configurator, error) {
	topology := runenv.StringParam(TopologyParam)
	if topology == "" {
		topology = ConnectAllTopology
	}
	ops := make([]Configurator, 0)
	switch topology {
	case ConnectAllTopology:
		ops = append(ops, ConnectAll)
	case SeedTopology:
		ops = append(ops, SeedConfigurator)
	default:
		return nil, fmt.Errorf("unknown topology func: %s", topology)
	}
	return ops, nil
}

// Configurator is a function that arbitarily modifies the provided node
// configurations. It is used to generate the topology (which nodes are
// connected to which) of the network, along with making other arbitrary changes
// to the configs.
type Configurator func(nodes []ConsensusNodeMetaConfig) ([]ConsensusNodeMetaConfig, error)

var _ = Configurator(ConnectAll)

// ConnectAll is a Configurator that connects all nodes to each other via
// persistent peers.
func ConnectAll(nodes []ConsensusNodeMetaConfig) ([]ConsensusNodeMetaConfig, error) {
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].GlobalSequence < nodes[j].GlobalSequence
	})
	peerIDs := peerIDs(nodes)

	// For each node, generate the string that excludes its own P2PID
	for i, nodeConfig := range nodes {
		var filteredP2PIDs []string
		for _, pid := range peerIDs {
			if pid != nodeConfig.PeerID {
				filteredP2PIDs = append(filteredP2PIDs, pid)
			}
		}

		// Here you could put the concatenated string into another field in NodeConfig
		// or do whatever you want with it.
		nodeConfig.CmtConfig.P2P.PersistentPeers = strings.Join(filteredP2PIDs, ",")
		nodes[i] = nodeConfig
	}

	return nodes, nil
}

var _ = Configurator(SeedConfigurator)

// SeedConfigurator is a Configurator that finds and sets the seed node for the
// network. Note that this only supports a single seed node for the time being.
func SeedConfigurator(nodes []ConsensusNodeMetaConfig) ([]ConsensusNodeMetaConfig, error) {
	// find the seed node
	var seedNode ConsensusNodeMetaConfig
	for i, node := range nodes {
		if node.GroupID == SeedGroupID {
			seedNode = node
			nodes[i].CmtConfig.P2P.SeedMode = true
			break
		}
		if seedNode.PeerID == "" {
			return nil, fmt.Errorf("expected at one seed node, found none")
		}
	}
	// add the seed node to all of the peers
	for i, node := range nodes {
		if node.GroupID != SeedGroupID {
			node.CmtConfig.P2P.Seeds = strings.Join([]string{seedNode.PeerID}, ",")
			nodes[i] = node
		}
	}
	return nodes, nil
}

package network

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sort"

	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	srvconfig "github.com/cosmos/cosmos-sdk/server/config"
	tmconfig "github.com/tendermint/tendermint/config"
	cmtjson "github.com/tendermint/tendermint/libs/json"
	coretypes "github.com/tendermint/tendermint/types"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/sync"
)

const (
	FinishedConfigState = sync.State("finished-config")
)

var (
	GenesisTopic = sync.NewTopic("genesis", map[string]json.RawMessage{})
	// NetworkConfigTopic is the topic used to exchange network configuration
	// between test instances.
	TestgroundConfigTopic = sync.NewTopic("testground_config", TestgroundConfig{})
	PeerPacketTopic       = sync.NewTopic("peer_packet", PeerPacket{})
	CommandTopic          = sync.NewTopic("command", Command{})
)

// PeerPacket is the message that is sent to other nodes upon network
// initialization. It contains the necessary info from this node to start the
// network.
type PeerPacket struct {
	PeerID          string          `json:"peer_id"`
	IP              string          `json:"ip"`
	GroupID         string          `json:"group_id"`
	GlobalSequence  int64           `json:"global_sequence"`
	GenesisAccounts []string        `json:"genesis_accounts"`
	GenesisPubKeys  []string        `json:"pub_keys"`
	GenTx           json.RawMessage `json:"gen_tx"`
}

func (pp *PeerPacket) IsValidator() bool {
	return pp.GroupID == ValidatorGroupID
}

func (pp *PeerPacket) IsLeader() bool {
	return pp.GlobalSequence == LeaderGlobalSequence
}

func (pp *PeerPacket) Name() string {
	return NodeID(pp.GlobalSequence)
}

func (pp *PeerPacket) GetPubKeys() ([]cryptotypes.PubKey, error) {
	pks := make([]cryptotypes.PubKey, 0, len(pp.GenesisPubKeys))
	for _, pk := range pp.GenesisPubKeys {
		sdkpk, err := DeserializeAccountPublicKey(pk)
		if err != nil {
			return nil, err
		}
		pks = append(pks, sdkpk)
	}
	return pks, nil
}

func NewTestgroundConfig(params *Params, genesis *coretypes.GenesisDoc, pps []PeerPacket) (TestgroundConfig, error) {
	genBytes, err := cmtjson.MarshalIndent(genesis, "", "  ")
	if err != nil {
		return TestgroundConfig{}, err
	}

	cfg := TestgroundConfig{
		Genesis:              genBytes,
		ConsensusNodeConfigs: make(map[string]ConsensusNodeMetaConfig),
	}
	for _, pp := range pps {
		cfg.ConsensusNodeConfigs[pp.Name()] = ConsensusNodeMetaConfig{
			CmtConfig:  StandardCometConfig(params),
			AppConfig:  StandardAppConfig(params),
			PeerPacket: pp,
		}
	}
	return cfg, nil
}

// TestgroundConfig is the first message sent by the Leader to the rest of the
// Follower nodes after the network has been configured.
type TestgroundConfig struct {
	Genesis              json.RawMessage `json:"genesis"`
	ConsensusNodeConfigs map[string]ConsensusNodeMetaConfig
}

type ConsensusNodeMetaConfig struct {
	PeerPacket PeerPacket        `json:"peer_packet"`
	CmtConfig  *tmconfig.Config  `json:"cmt_config"`
	AppConfig  *srvconfig.Config `json:"app_config"`
}

// Nodes returns the list of nodes in the network sorted by global sequence.
func (tcfg *TestgroundConfig) Nodes() []ConsensusNodeMetaConfig {
	nodes := make([]ConsensusNodeMetaConfig, 0, len(tcfg.ConsensusNodeConfigs))
	for _, n := range tcfg.ConsensusNodeConfigs {
		nodes = append(nodes, n)
	}
	sort.Slice(nodes, func(i, j int) bool {
		return nodes[i].PeerPacket.GlobalSequence < nodes[j].PeerPacket.GlobalSequence
	})
	return nodes
}

func peerIDs(nodes []ConsensusNodeMetaConfig) []string {
	peerIDs := make([]string, 0, len(nodes))
	for _, nodeCfg := range nodes {
		peerIDs = append(peerIDs, nodeCfg.PeerPacket.PeerID)
	}
	return peerIDs
}

func mapNodes(nodes []ConsensusNodeMetaConfig) map[string]ConsensusNodeMetaConfig {
	m := make(map[string]ConsensusNodeMetaConfig)
	for _, node := range nodes {
		m[node.PeerPacket.Name()] = node
	}
	return m
}

func PublishTestgroundConfig(ctx context.Context, initCtx *run.InitContext, cfg TestgroundConfig) error {
	_, err := initCtx.SyncClient.Publish(ctx, TestgroundConfigTopic, cfg)
	return err
}

func DownloadTestgroundConfig(ctx context.Context, initCtx *run.InitContext) (TestgroundConfig, error) {
	cfgs, err := DownloadSync(ctx, initCtx, TestgroundConfigTopic, TestgroundConfig{}, 1)
	if err != nil {
		return TestgroundConfig{}, err
	}
	if len(cfgs) != 1 {
		return TestgroundConfig{}, errors.New("no network config was downloaded despite there not being an error")
	}
	return cfgs[0], nil
}

func DownloadSync[T any](ctx context.Context, initCtx *run.InitContext, topic *sync.Topic, t T, count int) ([]T, error) {
	ch := make(chan T)
	sub, err := initCtx.SyncClient.Subscribe(ctx, topic, ch)
	if err != nil {
		return nil, err
	}

	output := make([]T, 0, count)
	for i := 0; i < count; i++ {
		select {
		case err := <-sub.Done():
			if err != nil {
				return nil, err
			}
			return output, errors.New("subscription was closed before receiving the expected number of messages")
		case o := <-ch:
			output = append(output, o)
		}
	}
	return output, nil
}

func NodeID(globalSeq int64) string {
	return fmt.Sprintf("%d", globalSeq)
}

package network

import (
	"context"
	"encoding/hex"
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
	"github.com/testground/sdk-go/runtime"
	"github.com/testground/sdk-go/sync"
)

const (
	FinishedConfigState = sync.State("finished-config")
	wrapperParts        = 20
)

var (
	// PeerPacketTopic is the topic that the peer packets are published to. This
	// is the first piece of information that is published.
	PeerPacketTopic = sync.NewTopic("peer_packet", PeerPacket{})
	// GenesisTopic is the topic that the genesis is published to.
	GenesisTopic = sync.NewTopic("genesis", GenesisWrapper{})
	// MetaConfigTopic is the topic where each node's app and tendermint configs
	// are puslished to. These are published before the any node starts.
	MetaConfigTopic = sync.NewTopic("meta_config", RoleConfig{})
	// CommandTopic is the topic that commands are published to. These commands
	// are published by the leader and subscribed to by all followers.
	CommandTopic = sync.NewTopic("command", Command{})
)

// PeerPacket is the message that is sent to other nodes upon network
// initialization. It contains the necessary info from this node to start the
// network.
type PeerPacket struct {
	PeerID          string          `json:"peer_id"`
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

func NewConfigSet(params *Params, pps []PeerPacket) []RoleConfig {
	nodeConfigs := make([]RoleConfig, 0, len(pps))

	for _, pp := range pps {
		nodeConfigs = append(nodeConfigs, RoleConfig{
			CmtConfig:      StandardCometConfig(params),
			AppConfig:      StandardAppConfig(params),
			PeerID:         pp.PeerID,
			GroupID:        pp.GroupID,
			GlobalSequence: pp.GlobalSequence,
		})
	}

	return nodeConfigs
}

type RoleConfig struct {
	PeerID         string            `json:"peer_id"`
	GroupID        string            `json:"group_id"`
	GlobalSequence int64             `json:"global_sequence"`
	CmtConfig      *tmconfig.Config  `json:"cmt_config"`
	AppConfig      *srvconfig.Config `json:"app_config"`
}

func peerIDs(nodes []RoleConfig) []string {
	peerIDs := make([]string, 0, len(nodes))
	for _, nodeCfg := range nodes {
		peerIDs = append(peerIDs, nodeCfg.PeerID)
	}
	return peerIDs
}

func searchNodes(nodes []RoleConfig, globalSeq int64) (RoleConfig, bool) {
	for _, node := range nodes {
		if node.GlobalSequence == globalSeq {
			return node, true
		}
	}
	return RoleConfig{}, false
}

func PublishNodeConfigs(ctx context.Context, initCtx *run.InitContext, nodes []RoleConfig) error {
	for _, node := range nodes {
		_, err := initCtx.SyncClient.Publish(ctx, MetaConfigTopic, node)
		if err != nil {
			return err
		}
	}

	return nil
}

func DownloadNodeConfigs(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) ([]RoleConfig, error) {
	return DownloadSync(ctx, initCtx, MetaConfigTopic, RoleConfig{}, runenv.TestInstanceCount)
}

// GenesisWrapper is a simple struct wrapper that makes it easier testground to
// properly serialize and distribute the genesis file.
type GenesisWrapper struct {
	// Part is the index of the part of the genesis file. This is used to bypass
	// testground's 32Kb limit on messages.
	Part    int    `json:"part"`
	Genesis string `json:"genesis"`
}

// PublishGenesis publishes the genesis to the sync service. It splits the
// genesis into 10 parts and publishes each part separately. This gets around the
// 32Kb limit the underlying websocket has
func PublishGenesis(ctx context.Context, initCtx *run.InitContext, gen *coretypes.GenesisDoc) error {
	genBytes, err := cmtjson.Marshal(gen)
	if err != nil {
		return err
	}

	wrappers := make([]GenesisWrapper, 0, wrapperParts)
	partSize := len(genBytes) / wrapperParts

	for i := 0; i < wrapperParts; i++ {
		start := i * partSize
		end := start + partSize
		if i == wrapperParts-1 {
			end = len(genBytes)
		}

		wrappers = append(wrappers, GenesisWrapper{
			Part:    i,
			Genesis: hex.EncodeToString(genBytes[start:end]),
		})
	}

	for _, wrapper := range wrappers {
		_, err = initCtx.SyncClient.Publish(ctx, GenesisTopic, wrapper)
		if err != nil {
			return err
		}
	}

	return nil
}

func DownloadGenesis(ctx context.Context, initCtx *run.InitContext) (json.RawMessage, error) {
	rawGens, err := DownloadSync(ctx, initCtx, GenesisTopic, GenesisWrapper{}, wrapperParts)
	if err != nil {
		return nil, err
	}
	// sort the genesis parts by their part number
	sort.Slice(rawGens, func(i, j int) bool {
		return rawGens[i].Part < rawGens[j].Part
	})
	var genesis []byte
	for _, rawGen := range rawGens {
		bz, err := hex.DecodeString(rawGen.Genesis)
		if err != nil {
			return nil, err
		}
		genesis = append(genesis, bz...)
	}
	return genesis, nil
}

// DownloadSync downloads the given topic from the sync service. It will
// download the given number of messages from the topic. If the topic is closed
// before the expected number of messages are received, an error is returned.
func DownloadSync[T any](ctx context.Context, initCtx *run.InitContext, topic *sync.Topic, _ T, count int) ([]T, error) {
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

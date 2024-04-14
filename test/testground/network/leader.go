package network

import (
	"context"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/celestiaorg/celestia-app/app"
	"github.com/celestiaorg/celestia-app/pkg/user"
	"github.com/celestiaorg/celestia-app/test/txsim"
	"github.com/celestiaorg/celestia-app/test/util/genesis"
	"github.com/celestiaorg/celestia-app/test/util/testfactory"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	v1 "github.com/cosmos/cosmos-sdk/x/gov/types/v1"
	oldgov "github.com/cosmos/cosmos-sdk/x/gov/types/v1beta1"
	"github.com/cosmos/cosmos-sdk/x/params/types/proposal"
	cmtjson "github.com/tendermint/tendermint/libs/json"
	coretypes "github.com/tendermint/tendermint/types"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

// Leader is the role for the leader node in a test. It is responsible for
// creating the genesis block and distributing it to all nodes.
type Leader struct {
	*ConsensusNode
	signer *user.Signer
}

// Plan is the method that creates and distributes the genesis, configurations,
// and keys for all of the other nodes in the network.
func (l *Leader) Plan(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	runenv.RecordMessage("Bootstrapping")
	packets, err := l.Bootstrap(ctx, runenv, initCtx)
	if err != nil {
		return err
	}

	runenv.RecordMessage("got packets, using parts for the genesis")

	// create Genesis and distribute it to all nodes
	genesis, err := l.GenesisEvent(runenv, l.params, packets)
	if err != nil {
		return err
	}

	err = PublishGenesis(ctx, initCtx, genesis)
	if err != nil {
		runenv.RecordMessage("it is the genesis publications")
		return err
	}

	runenv.RecordMessage("published genesis")

	nodes := NewConfigSet(l.params, packets)

	// apply the configurator functions to the testground config. This step is
	// responsible for hardcoding any topolgy
	for _, configurator := range l.params.Configurators {
		nodes, err = configurator(nodes)
		if err != nil {
			return err
		}
	}

	runenv.RecordMessage("applied configurators")

	err = PublishNodeConfigs(ctx, initCtx, nodes)
	if err != nil {
		return err
	}

	node, has := searchNodes(nodes, initCtx.GlobalSeq)
	if !has {
		return errors.New("node not found")
	}

	genBytes, err := cmtjson.MarshalIndent(genesis, "", "  ")
	if err != nil {
		return err
	}

	err = l.Init(homeDir, genBytes, node)
	if err != nil {
		return err
	}

	if l.CmtConfig.Instrumentation.PyroscopeTrace {
		runenv.RecordMessage("pyroscope: follower starting pyroscope")
	}

	err = addPeersToAddressBook(l.CmtConfig.P2P.AddrBookFile(), packets)
	if err != nil {
		return err
	}

	err = l.ConsensusNode.StartNode(ctx, l.baseDir)
	if err != nil {
		return err
	}

	runenv.RecordMessage("waiting for initial height")

	_, err = l.cctx.WaitForHeightWithTimeout(int64(5), time.Minute*7)
	if err != nil {
		return err
	}

	addr := testfactory.GetAddress(l.cctx.Keyring, l.Name)

	signer, err := user.SetupSigner(ctx, l.cctx.Keyring, l.cctx.GRPCClient, addr, l.ecfg)
	if err != nil {
		runenv.RecordMessage(fmt.Sprintf("leader: failed to setup signer %+v", err))
		return err
	}
	l.signer = signer

	// this is a helpful sanity check that logs the blocks from the POV of the
	// leader in a testground viewable way.
	//nolint:errcheck
	go l.subscribeAndRecordBlocks(ctx, runenv)

	return nil
}

func (l *Leader) Execute(ctx context.Context, runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	defer func() {
		_, err := initCtx.SyncClient.Publish(ctx, CommandTopic, EndTestCommand())
		if err != nil {
			runenv.RecordMessage(fmt.Sprintf("error publishing end test command: %v", err))
		}
	}()

	switch l.params.Experiment {
	case UnboundedBlockSize:
		runenv.RecordMessage(fmt.Sprintf("leader running experiment %s", l.params.Experiment))
		err := l.unboundedBlockSize(ctx, runenv, initCtx, l.ecfg.Codec, 10)
		if err != nil {
			runenv.RecordMessage(fmt.Sprintf("error unbounded block size test: %v", err))
		}
	case ConsistentFill:
		runenv.RecordMessage(fmt.Sprintf("leader running experiment %s", l.params.Experiment))
		args, err := fillBlocks(ctx, runenv, initCtx, time.Minute*20)
		if err != nil {
			runenv.RecordMessage(fmt.Sprintf("error consistent fill block size test: %v", err))
		}
		go l.RunTxSim(ctx, args)
	default:
		return fmt.Errorf("unknown experiment %s", l.params.Experiment)
	}

	runenv.RecordMessage(fmt.Sprintf("leader waiting for halt height %d", l.params.HaltHeight))

	_, err := l.cctx.WaitForHeightWithTimeout(int64(l.params.HaltHeight), time.Minute*50)
	if err != nil {
		return err
	}

	return err
}

// Retro collects standard data from the leader node and saves it as a file.
// This data includes the block times, rounds required to reach consensus, and
// the block sizes.
func (l *Leader) Retro(ctx context.Context, runenv *runtime.RunEnv, _ *run.InitContext) error {
	//nolint:errcheck
	defer l.ConsensusNode.Stop()

	blockRes, err := l.cctx.Client.Header(ctx, nil)
	if err != nil {
		return err
	}

	maxBlockSize := 0
	for i := int64(1); i < blockRes.Header.Height; i++ {
		blockRes, err := l.cctx.Client.Block(ctx, nil)
		if err != nil {
			return err
		}
		size := blockRes.Block.Size()
		if size > maxBlockSize {
			maxBlockSize = size
		}
	}

	runenv.RecordMessage(fmt.Sprintf("leader retro: height %d max block size bytes %d", blockRes.Header.Height, maxBlockSize))

	return nil
}

func (l *Leader) GenesisEvent(runevn *runtime.RunEnv, params *Params, packets []PeerPacket) (*coretypes.GenesisDoc, error) {
	pubKeys := make([]cryptotypes.PubKey, 0)
	addrs := make([]string, 0)
	gentxs := make([]json.RawMessage, 0, len(packets))

	for _, packet := range packets {
		pks, err := packet.GetPubKeys()
		if err != nil {
			return nil, err
		}
		pubKeys = append(pubKeys, pks...)
		addrs = append(addrs, packet.GenesisAccounts...)
		if packet.GroupID == ValidatorGroupID {
			gentxs = append(gentxs, packet.GenTx)
			runevn.RecordMessage(fmt.Sprintf("leader: added gentx %s", packet.PeerID))
		}
	}

	return genesis.Document(
		l.ecfg,
		TestgroundConsensusParams(params),
		l.params.ChainID,
		gentxs,
		addrs,
		pubKeys,
		params.GenesisModifiers...,
	)
}

func SerializePublicKey(pubKey cryptotypes.PubKey) string {
	return hex.EncodeToString(pubKey.Bytes())
}

func DeserializeAccountPublicKey(hexPubKey string) (cryptotypes.PubKey, error) {
	bz, err := hex.DecodeString(hexPubKey)
	if err != nil {
		return nil, err
	}

	var pubKey secp256k1.PubKey
	if len(bz) != secp256k1.PubKeySize {
		return nil, errors.New("incorrect pubkey size")
	}

	pubKey.Key = bz

	return &pubKey, nil
}

// changeParams submits a parameter change proposal to the network. Errors are
// thrown if the proposal is not submitted successfully.
func (l *Leader) changeParams(ctx context.Context, runenv *runtime.RunEnv, propID uint64, changes ...proposal.ParamChange) error {
	content := proposal.NewParameterChangeProposal("title", "description", changes)
	addr := testfactory.GetAddress(l.cctx.Keyring, l.Name)

	propMsg, err := oldgov.NewMsgSubmitProposal(
		content,
		sdk.NewCoins(
			sdk.NewCoin(app.BondDenom, sdk.NewInt(1000000000))),
		addr,
	)
	if err != nil {
		runenv.RecordMessage(fmt.Sprintf("leader: failed to create proposal msg %+v", err))
		return err
	}

	voteMsg := v1.NewMsgVote(addr, propID, v1.VoteOption_VOTE_OPTION_YES, "")

	resp, err := l.signer.SubmitTx(ctx, []sdk.Msg{propMsg, voteMsg}, user.SetGasLimitAndFee(1000000, 0.2))
	if err != nil {
		runenv.RecordMessage(fmt.Sprintf("leader: failed to submit tx %+v, %v", changes, err))
		return err
	}

	if resp.Code != 0 {
		runenv.RecordMessage(fmt.Sprintf("leader: failed to submit tx %+v, %v %v", changes, resp.Code, resp.Codespace))
		return fmt.Errorf("proposal failed with code %d: %s", resp.Code, resp.RawLog)
	}

	runenv.RecordMessage(fmt.Sprintf("leader: submitted successful proposal %+v", changes))

	return nil
}

// subscribeAndRecordBlocks subscribes to the block event stream and records
// the block times and sizes.
func (l *Leader) subscribeAndRecordBlocks(ctx context.Context, runenv *runtime.RunEnv) error {
	runenv.RecordMessage("leader: subscribing to block events")
	query := "tm.event = 'NewBlock'"
	events, err := l.cctx.Client.Subscribe(ctx, "leader", query, 10)
	if err != nil {
		return err
	}

	lastBlockTime := time.Now()

	for {
		select {
		case ev := <-events:
			newBlock, ok := ev.Data.(coretypes.EventDataNewBlock)
			if !ok {
				return fmt.Errorf("unexpected event type: %T", ev.Data)
			}
			blockTime := newBlock.Block.Time.Sub(lastBlockTime)
			runenv.RecordMessage(fmt.Sprintf("leader height %d time %v size bytes %d app version %v", newBlock.Block.Height, blockTime, newBlock.Block.Size(), newBlock.Block.Version.App))
			lastBlockTime = newBlock.Block.Time
		case <-ctx.Done():
			return nil
		}
	}
}

func (l *Leader) RunTxSim(ctx context.Context, c RunTxSimCommandArgs) error {
	grpcEndpoint := "127.0.0.1:9090"
	opts := txsim.DefaultOptions().UseFeeGrant().SuppressLogs()
	return txsim.Run(ctx, grpcEndpoint, l.kr, l.ecfg, opts, c.Sequences()...)
}

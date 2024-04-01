package sanity

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/celestiaorg/celestia-app/test/testground/compositions"
	"github.com/celestiaorg/celestia-app/test/testground/network"
	"github.com/tendermint/tendermint/config"
	tmlog "github.com/tendermint/tendermint/libs/log"
	"github.com/tendermint/tendermint/p2p"
	"github.com/tendermint/tendermint/p2p/conn"
	"github.com/tendermint/tendermint/pkg/trace"
	"github.com/testground/sdk-go/run"
	"github.com/testground/sdk-go/runtime"
)

const FailedState = "failed"

const (
	influxURL = "http://51.158.232.250:8086"
	influxKey = "SgmlSaqxiR6ZTmBhyR5E0C9Nf_x35AoxeLyn4NE5jYBlMFIPDHmNBE_levqq4UBnjfoJXXYYxkha7F3GUWki9w=="
)

// EntryPoint is the universal entry point for all role based tests.
func EntryPoint(runenv *runtime.RunEnv, initCtx *run.InitContext) error {
	runenv.RecordMessage("running sanity test, hello")
	initCtx, ctx, cancel, err := compositions.InitTest(runenv, initCtx)
	if err != nil {
		runenv.RecordFailure(err)
		initCtx.SyncClient.MustSignalAndWait(ctx, FailedState, runenv.TestInstanceCount)
		return err
	}
	defer cancel()

	runenv.RecordMessage("initialized experiment node, starting role")

	// bootstrap by downloading all peer's nodes
	ip, err := initCtx.NetClient.GetDataNetworkIP()
	if err != nil {
		return err
	}

	cfg := config.DefaultP2PConfig()
	mcfg := conn.DefaultMConnConfig()
	mcfg.RecvRate = 100_000_000
	mcfg.SendRate = 100_000_000

	icfg := config.DefaultInstrumentationConfig()
	icfg.InfluxBucket = "testground"
	icfg.InfluxURL = influxURL
	icfg.InfluxToken = influxKey
	icfg.InfluxBatchSize = 100
	icfg.InfluxTables = strings.Join([]string{"totals, bandwidth"}, ",")

	iclient, err := trace.NewClient(icfg, tmlog.NewNopLogger(), "sanity", fmt.Sprintf("%d", initCtx.GlobalSeq))
	if err != nil {
		return err
	}

	reactor1 := NewMockReactor(defaultTestChannels)
	node, err := NewNode(*cfg, mcfg, ip, reactor1)
	if err != nil {
		return err
	}
	node.start()

	runenv.RecordMessage("node started at", ip.String())

	pp := network.PeerPacket{
		PeerID:         node.PeerAddress(),
		RPC:            ip.String(),
		GroupID:        runenv.TestGroupID,
		GlobalSequence: initCtx.GlobalSeq,
	}

	_, err = initCtx.SyncClient.Publish(ctx, network.PeerPacketTopic, pp)
	if err != nil {
		return err
	}

	packets, err := network.DownloadSync(ctx, initCtx, network.PeerPacketTopic, network.PeerPacket{}, runenv.TestInstanceCount)
	if err != nil {
		return err
	}

	externalPeerAddr := ""
	for _, packet := range packets {
		if packet.PeerID != node.PeerAddress() {
			externalPeerAddr = packet.PeerID
			break
		}
		runenv.RecordMessage(fmt.Sprintf("packet: %+v", packet.PeerID))
	}

	nodeID, pip, _, err := parsePeerID(externalPeerAddr)
	if err != nil {
		return err
	}

	pa := p2p.NetAddress{
		ID:   p2p.ID(nodeID),
		IP:   pip,
		Port: 26656,
	}

	runenv.RecordMessage("dialing peer with address: %s", pa.String())
	err = node.sw.DialPeerWithAddress(&pa)
	if err != nil {
		return err
	}

	runenv.RecordMessage("deploying more bytes, hold steady lads")
	var wg sync.WaitGroup
	reactor1.FloodChannel(&wg, FirstChannel, time.Second*20)
	reactor1.FloodChannel(&wg, SecondChannel, time.Second*20)
	reactor1.FloodChannel(&wg, ThirdChannel, time.Second*20)
	reactor1.FloodChannel(&wg, FourthChannel, time.Second*20)
	reactor1.FloodChannel(&wg, FifthChannel, time.Second*20)
	reactor1.FloodChannel(&wg, SixthChannel, time.Second*20)
	reactor1.FloodChannel(&wg, SeventhChannel, time.Second*20)
	reactor1.FloodChannel(&wg, EighthChannel, time.Second*20)
	wg.Wait()

	// wait to ensure that all messages were sent
	time.Sleep(time.Second * 5)

	// calculate the bandwidth
	bandwidths := CalculateBandwdiths(reactor1.Traces)

	iclient.WritePoint("totals", map[string]interface{}{
		"bandwidth": bandwidths},
	)

	runenv.RecordMessage(fmt.Sprintf("NODE REPORT: %d bandwidths: %v", initCtx.GlobalSeq, bandwidths))

	for i, t := range reactor1.Traces {
		iclient.WritePoint("bandwidth", map[string]interface{}{
			"index": i,
			"trace": t,
		},
		)
	}

	time.Sleep(time.Second * 20)
	// signal that the test has completed successfully
	runenv.RecordSuccess()
	return nil
}

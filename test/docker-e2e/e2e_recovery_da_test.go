package docker_e2e

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	appda "github.com/celestiaorg/celestia-app/v11/pkg/da"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	da "github.com/celestiaorg/tastora/framework/docker/dataavailability"
	"github.com/celestiaorg/tastora/framework/testutil/wait"
	mobyclient "github.com/moby/moby/client"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
)

// TestSurgeryDA halts the validators, clears the halt with the recovery script
// and checks that the bridge and light nodes follow the chain without a restart.
func (s *CelestiaTestSuite) TestSurgeryDA() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping in short mode")
	}

	ctx := context.TODO()
	env := s.startRecoveryDA(ctx, min(appconsts.Version, celestiaNodeMaxAppVersion))
	validators := env.celestia.Validators

	haltHeight := s.armHaltHeight(ctx, validators, env.fullNode, env.fullNodePeer)
	t.Logf("Halt at height %d", haltHeight)
	halt := s.waitForHalt(ctx, validators, env.fullNode, haltHeight)
	s.requireHalted(ctx, env.fullNode, haltHeight)
	// When a validator halted before its peers saw its precommit, no other node
	// committed the halt height and there is no canonical block for it yet.
	haltBlockHash := ""
	if halt.fullNodeHeight == haltHeight {
		haltBlockHash = s.block(ctx, env.fullNode, haltHeight).BlockID.Hash.String()
	}

	// The bridge never stores the block its core did not execute.
	bridgeHead, err := daHead(ctx, env.bridge)
	s.Require().NoError(err, "failed to get the bridge head")
	s.Require().Less(bridgeHead, haltHeight, "bridge stored the halt block")

	// The script, not the test, clears the halt height.
	for _, v := range validators {
		out, err := s.runRecoveryScript(ctx, v, "restart", "--setup", "none", "--home", v.HomeDir())
		t.Logf("recovery.sh restart on %s:\n%s", v.Name(), out)
		s.Require().NoError(err, "restart failed on %s", v.Name())
		s.Require().Contains(out, "config/app.toml", "recovery.sh did not clear the halt on %s", v.Name())
		s.Require().NoError(v.Restart(ctx), "failed to restart %s", v.Name())
	}

	s.waitForHeight(ctx, env.celestia.Nodes(), haltHeight+5)
	if haltBlockHash != "" {
		s.requireBlockHash(ctx, env.celestia.Nodes(), haltHeight, haltBlockHash)
	}
	s.verifyAppHash(ctx, validators, env.fullNode, haltHeight+1)
	s.Require().NoError(s.CheckLiveness(ctx, env.celestia), "validator liveness check failed")

	s.requireDARecovered(ctx, env, haltHeight)
	for h := haltHeight - 1; h <= haltHeight+1; h++ {
		s.requireBridgeEDS(ctx, env, validators[0], h)
	}
}

// TestRollbackDA halts the validators, rolls back executed heights with the
// recovery script and checks that the bridge and light nodes keep the canonical blocks.
func (s *CelestiaTestSuite) TestRollbackDA() {
	t := s.T()
	if testing.Short() {
		t.Skip("skipping in short mode")
	}
	appVersion := min(appconsts.Version, celestiaNodeMaxAppVersion)
	if appVersion < appconsts.Version {
		t.Skipf("celestia-appd rollback needs app version %d, DA nodes accept up to %d", appconsts.Version, appVersion)
	}

	ctx := context.TODO()
	env := s.startRecoveryDA(ctx, appVersion)
	validators := env.celestia.Validators

	haltHeight := s.armHaltHeight(ctx, validators, env.fullNode, env.fullNodePeer)
	t.Logf("Halt at height %d and roll back %d heights", haltHeight, rollbackDepth)
	halt := s.waitForHalt(ctx, validators, env.fullNode, haltHeight)
	s.requireHalted(ctx, env.fullNode, haltHeight)

	target := haltHeight - 1 - rollbackDepth
	canonical := make(map[int64]string)
	for h := target + 1; h <= halt.fullNodeHeight; h++ {
		canonical[h] = s.block(ctx, env.fullNode, h).BlockID.Hash.String()
	}
	// Both DA nodes store the window up to the last executed height before the rollback.
	s.waitForDAHead(ctx, env.daNodes, haltHeight-2)
	for h := target + 1; h < haltHeight; h++ {
		s.requireDABlockHash(ctx, env.daNodes, h, canonical[h])
	}

	for _, v := range validators {
		s.recoveryRollback(ctx, v, target)
		s.restartWithHaltHeight(ctx, v, 0, env.fullNodePeer)
	}

	s.waitForHeight(ctx, env.celestia.Nodes(), haltHeight+10)
	// If no node committed the halt height before the halt, it was decided after the restart.
	if _, ok := canonical[haltHeight]; !ok {
		canonical[haltHeight] = s.block(ctx, env.fullNode, haltHeight).BlockID.Hash.String()
	}
	for h := target + 1; h <= haltHeight; h++ {
		s.requireBlockHash(ctx, validators, h, canonical[h])
	}
	s.verifyAppHash(ctx, validators, env.fullNode, target+2)
	s.Require().NoError(s.CheckLiveness(ctx, env.celestia), "validator liveness check failed")

	// The bridge's core re-executed these heights and announced them again.
	s.waitForDAHead(ctx, env.daNodes, haltHeight)
	for h := target + 1; h <= haltHeight; h++ {
		s.requireDABlockHash(ctx, env.daNodes, h, canonical[h])
		s.requireBridgeEDS(ctx, env, validators[0], h)
	}
	s.requireDARecovered(ctx, env, haltHeight)
}

// recoveryDA is a recovery chain with a bridge and a light node and the blobs submitted before the halt.
type recoveryDA struct {
	celestia     *cosmos.Chain
	fullNode     *cosmos.ChainNode
	fullNodePeer string
	daNetwork    *da.Network
	bridge       *da.Node
	daNodes      []*da.Node
	txClient     *user.TxClient
	blobs        []blobData
	containers   map[string]daContainer
}

// startRecoveryDA starts a recovery chain at appVersion with a bridge on validators[0]
// and a light node, then submits blobs and checks that the light node serves them.
func (s *CelestiaTestSuite) startRecoveryDA(ctx context.Context, appVersion uint64) recoveryDA {
	cfg := dockerchain.DefaultConfig(s.client, s.network)
	cfg.Genesis = cfg.Genesis.WithAppVersion(appVersion)
	celestia, fullNode, fullNodePeer := s.startRecoveryChain(ctx, cfg)

	daNetwork := s.DeployDANetwork(ctx, celestia, s.client, s.network)
	bridge := daNetwork.GetBridgeNodes()[0]
	light := daNetwork.GetLightNodes()[0]

	// The full node never restarts, so its gRPC connection stays valid.
	txClient, err := dockerchain.SetupTxClient(ctx, fullNode, cfg)
	s.Require().NoError(err, "failed to setup TxClient")

	blobs := s.submitBlobTransactions(ctx, txClient, 3)
	s.waitForDAHead(ctx, []*da.Node{light}, blobs[len(blobs)-1].height)
	s.verifyBlobRetrieval(ctx, daNetwork, blobs)
	daNodes := []*da.Node{bridge, light}
	return recoveryDA{
		celestia:     celestia,
		fullNode:     fullNode,
		fullNodePeer: fullNodePeer,
		daNetwork:    daNetwork,
		bridge:       bridge,
		daNodes:      daNodes,
		txClient:     txClient,
		blobs:        blobs,
		containers:   s.daContainers(ctx, daNodes),
	}
}

// requireDARecovered checks that the DA nodes follow the chain past the halt with
// the full node's blocks, serve old and new blobs, and were never restarted.
func (s *CelestiaTestSuite) requireDARecovered(ctx context.Context, env recoveryDA, haltHeight int64) {
	light := env.daNetwork.GetLightNodes()[0]
	s.waitForDAHead(ctx, env.daNodes, haltHeight+5)
	s.waitForSampled(ctx, light, haltHeight+5)
	heights := []int64{haltHeight - 1, haltHeight, haltHeight + 1, haltHeight + 5}
	for _, b := range env.blobs {
		heights = append(heights, b.height)
	}
	for _, h := range heights {
		s.requireDABlockHash(ctx, env.daNodes, h, s.block(ctx, env.fullNode, h).BlockID.Hash.String())
	}

	newBlobs := s.submitBlobTransactions(ctx, env.txClient, 1)
	s.Require().Greater(newBlobs[0].height, haltHeight, "new blob is not above the halt height")
	s.waitForDAHead(ctx, []*da.Node{light}, newBlobs[0].height)
	s.verifyBlobRetrieval(ctx, env.daNetwork, newBlobs)
	s.verifyBlobRetrieval(ctx, env.daNetwork, env.blobs)
	s.requireDAUntouched(ctx, env.daNodes, env.containers)
}

// requireBridgeEDS rebuilds the EDS of block height from node and checks it against
// the block's data hash and the DAH and EDS the bridge stores.
func (s *CelestiaTestSuite) requireBridgeEDS(ctx context.Context, env recoveryDA, node *cosmos.ChainNode, height int64) {
	block := s.block(ctx, node, height).Block
	eds, err := appda.ConstructEDS(block.Txs.ToSliceOfBytes(), block.Version.App, -1)
	s.Require().NoError(err, "failed to construct the EDS of block %d", height)
	dah, err := appda.NewDataAvailabilityHeader(eds)
	s.Require().NoError(err, "failed to build the DAH of block %d", height)
	s.Require().Equal([]byte(block.DataHash), dah.Hash(), "data hash of block %d does not match its EDS", height)

	var h daHeader
	err = daCall(ctx, env.bridge, "header.GetByHeight", []any{height}, &h)
	s.Require().NoError(err, "failed to get header %d from the bridge", height)
	s.Require().Equal(dah.RowRoots, h.DAH.RowRoots, "row roots of block %d differ on the bridge", height)
	s.Require().Equal(dah.ColumnRoots, h.DAH.ColumnRoots, "column roots of block %d differ on the bridge", height)

	// The rsmt2d EDS JSON holds the flattened square in row order.
	var bridgeEDS struct {
		DataSquare [][]byte `json:"data_square"`
	}
	err = daCall(ctx, env.bridge, "share.GetEDS", []any{height}, &bridgeEDS)
	s.Require().NoError(err, "failed to get the EDS of block %d from the bridge", height)
	shares := eds.Flattened()
	s.Require().Len(bridgeEDS.DataSquare, len(shares), "EDS of block %d has a different size on the bridge", height)
	for i := range shares {
		s.Require().Equal(shares[i], bridgeEDS.DataSquare[i], "share %d of block %d differs on the bridge", i, height)
	}
}

// daHeader is the part of a DA node's extended header the tests use.
type daHeader struct {
	Header struct {
		Height string `json:"height"`
	} `json:"header"`
	Commit struct {
		BlockID struct {
			Hash string `json:"hash"`
		} `json:"block_id"`
	} `json:"commit"`
	DAH appda.DataAvailabilityHeader `json:"dah"`
}

// samplingStats is the part of the light node's sampling stats the tests use.
type samplingStats struct {
	SampledChainHead uint64         `json:"head_of_sampled_chain"`
	Failed           map[string]int `json:"failed"`
}

// daContainer identifies a DA node container and its last start.
type daContainer struct {
	id        string
	startedAt string
}

// daRPCTimeout bounds each DA JSON-RPC request.
const daRPCTimeout = 30 * time.Second

var daHTTPClient = &http.Client{Timeout: daRPCTimeout}

// daCall sends a JSON-RPC request to a DA node and decodes the result into out.
func daCall(ctx context.Context, n *da.Node, method string, params []any, out any) error {
	ctx, cancel := context.WithTimeout(ctx, daRPCTimeout)
	defer cancel()
	info, err := n.GetNetworkInfo(ctx)
	if err != nil {
		return err
	}
	body, err := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": 1, "method": method, "params": params})
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, "http://0.0.0.0:"+info.External.Ports.RPC, bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := daHTTPClient.Do(req)
	if err != nil {
		return err
	}
	defer func() { _ = resp.Body.Close() }()
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("%s on %s: unexpected status %s", method, n.Name(), resp.Status)
	}
	var res struct {
		Result json.RawMessage `json:"result"`
		Error  *struct {
			Code    int    `json:"code"`
			Message string `json:"message"`
		} `json:"error"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&res); err != nil {
		return err
	}
	if res.Error != nil {
		return fmt.Errorf("%s on %s: rpc error %d: %s", method, n.Name(), res.Error.Code, res.Error.Message)
	}
	return json.Unmarshal(res.Result, out)
}

// daHead returns the height of the DA node's local head.
func daHead(ctx context.Context, n *da.Node) (int64, error) {
	var h daHeader
	if err := daCall(ctx, n, "header.LocalHead", []any{}, &h); err != nil {
		return 0, err
	}
	return strconv.ParseInt(h.Header.Height, 10, 64)
}

// daBlockHash returns the block hash the DA node stores for height.
func daBlockHash(ctx context.Context, n *da.Node, height int64) (string, error) {
	var h daHeader
	if err := daCall(ctx, n, "header.GetByHeight", []any{height}, &h); err != nil {
		return "", err
	}
	return h.Commit.BlockID.Hash, nil
}

// requireDABlockHash checks that every DA node stores the given block hash at height.
func (s *CelestiaTestSuite) requireDABlockHash(ctx context.Context, nodes []*da.Node, height int64, want string) {
	for _, n := range nodes {
		got, err := daBlockHash(ctx, n, height)
		s.Require().NoError(err, "failed to get header %d from %s", height, n.Name())
		s.Require().Equal(want, got, "header %d differs on %s", height, n.Name())
	}
}

// waitForDAHead waits until every DA node's local head is above height.
func (s *CelestiaTestSuite) waitForDAHead(ctx context.Context, nodes []*da.Node, height int64) {
	for _, n := range nodes {
		err := wait.ForCondition(ctx, rollbackTimeout, 2*time.Second, func() (bool, error) {
			head, err := daHead(ctx, n)
			return head > height, err
		})
		s.Require().NoError(err, "%s did not pass height %d", n.Name(), height)
	}
}

// waitForSampled waits until the light node has sampled above height with no failed heights.
func (s *CelestiaTestSuite) waitForSampled(ctx context.Context, light *da.Node, height int64) {
	var last samplingStats
	err := wait.ForCondition(ctx, rollbackTimeout, 2*time.Second, func() (bool, error) {
		var stats samplingStats
		if err := daCall(ctx, light, "das.SamplingStats", []any{}, &stats); err != nil {
			return false, err
		}
		last = stats
		return stats.SampledChainHead > uint64(height) && len(stats.Failed) == 0, nil
	})
	s.Require().NoError(err, "%s did not sample past height %d: %+v", light.Name(), height, last)
}

// daContainers returns the container of each DA node by name and checks that it runs and was never restarted.
func (s *CelestiaTestSuite) daContainers(ctx context.Context, nodes []*da.Node) map[string]daContainer {
	containers := make(map[string]daContainer)
	for _, n := range nodes {
		res, err := s.client.ContainerInspect(ctx, n.ContainerLifecycle.ContainerName(), mobyclient.ContainerInspectOptions{})
		s.Require().NoError(err, "failed to inspect %s", n.Name())
		s.Require().True(res.Container.State.Running, "%s is not running", n.Name())
		s.Require().Zero(res.Container.RestartCount, "%s was restarted", n.Name())
		containers[n.Name()] = daContainer{id: res.Container.ID, startedAt: res.Container.State.StartedAt}
	}
	return containers
}

// requireDAUntouched checks that no DA node was recreated or restarted since before was taken.
func (s *CelestiaTestSuite) requireDAUntouched(ctx context.Context, nodes []*da.Node, before map[string]daContainer) {
	s.Require().Equal(before, s.daContainers(ctx, nodes), "a DA node was recreated or restarted")
}

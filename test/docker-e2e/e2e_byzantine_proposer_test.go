package docker_e2e

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v12/pkg/da"
	"github.com/celestiaorg/celestia-app/v12/test/util/genesis"
	"github.com/celestiaorg/celestia-app/v12/test/util/malicious"
	tastoracontainertypes "github.com/celestiaorg/tastora/framework/docker/container"
	"github.com/celestiaorg/tastora/framework/docker/cosmos"
	"github.com/celestiaorg/tastora/framework/testutil/wait"

	"celestiaorg/celestia-app/test/docker-e2e/dockerchain"
)

const (
	// maliciousImageEnvVar overrides the repository of the test-only image
	// built by `make build-docker-malicious`. Its tag is CELESTIA_TAG.
	maliciousImageEnvVar  = "CELESTIA_MALICIOUS_IMAGE"
	defaultMaliciousImage = "celestia-app-malicious"
	// maliciousStartHeight is the first height at which the byzantine
	// validator misbehaves. Blob load from txsim starts before it.
	maliciousStartHeight = 10
	// byzantineBlocks is how many blocks after maliciousStartHeight the test
	// observes. The byzantine validator has 1/4 of the voting power, so it is
	// the round 0 proposer of about a quarter of them.
	byzantineBlocks = 40
	// rejectedProposalLog is the message the honest ProcessProposal logs when
	// it rejects a block. Both behaviours make the data root check fail.
	rejectedProposalLog = "Rejected proposal block"
)

// byzantineBehavior is one misbehaviour of the byzantine proposer. To test a
// new one, add a handler to malicious.App.PrepareProposalHandlerMap and a row
// to byzantineBehaviors.
type byzantineBehavior struct {
	name    string
	handler string
	// everyProposalInvalid is true when every proposal the handler builds is
	// invalid, so no block from the byzantine validator may be committed. It
	// is false when some proposals stay valid, e.g. out of order blobs in a
	// block with fewer than two blob namespaces.
	everyProposalInvalid bool
}

var byzantineBehaviors = []byzantineBehavior{
	{name: "OutOfOrderBlobs", handler: malicious.OutOfOrderHandlerKey},
	{name: "BadDataRoot", handler: malicious.BadDataRootHandlerKey, everyProposalInvalid: true},
}

// TestByzantineProposer runs three honest validators and one byzantine
// validator under blob load. It checks that the honest validators reject the
// byzantine proposals, that every committed block has a valid data root, that
// the chain stays live and that the honest validators agree on the app hash.
func (s *CelestiaTestSuite) TestByzantineProposer() {
	if testing.Short() {
		s.T().Skip("skipping in short mode")
	}
	for _, behavior := range byzantineBehaviors {
		s.Run(behavior.name, func() { s.runByzantineProposer(behavior) })
	}
}

func (s *CelestiaTestSuite) runByzantineProposer(behavior byzantineBehavior) {
	t := s.T()
	ctx := context.Background()

	cfg := dockerchain.DefaultConfig(s.client, s.network)
	cfg.Genesis = cfg.Genesis.WithValidators(genesis.NewDefaultValidator("validator3"))
	nodeBuilders, err := dockerchain.NodeConfigBuilders(cfg)
	s.Require().NoError(err)
	s.Require().Len(nodeBuilders, 4)

	// The last validator is byzantine: 1 of 4 equal validators is < 1/3.
	byzantineIdx := len(nodeBuilders) - 1
	byzantineVal, ok := cfg.Genesis.Validator(byzantineIdx)
	s.Require().True(ok)
	byzantineAddr := byzantineVal.PrivateKey().Address
	nodeBuilders[byzantineIdx].
		WithImage(tastoracontainertypes.NewImage(maliciousImage(), cfg.Tag, "10001:10001")).
		WithEnvVars(
			"MALICIOUS_BEHAVIOR="+behavior.handler,
			fmt.Sprintf("MALICIOUS_START_HEIGHT=%d", maliciousStartHeight),
		)
	nodeConfigs := make([]cosmos.ChainNodeConfig, len(nodeBuilders))
	for i, b := range nodeBuilders {
		nodeConfigs[i] = b.Build()
	}

	celestia, err := dockerchain.NewCelestiaChainBuilder(t, cfg).WithNodes(nodeConfigs...).Build(ctx)
	s.Require().NoError(err, "failed to build chain")
	s.Require().NoError(celestia.Start(ctx), "failed to start chain")
	t.Cleanup(func() {
		if err := celestia.Remove(ctx); err != nil {
			t.Logf("Error removing chain: %v", err)
		}
	})

	honestNodes := celestia.Nodes()[:byzantineIdx]
	byzantineNode := celestia.Nodes()[byzantineIdx]
	s.startConsistencyMonitor(ctx, honestNodes)
	s.CreateTxSim(ctx, celestia)

	endHeight := int64(maliciousStartHeight + byzantineBlocks)
	s.Require().NoError(wait.ForCondition(ctx, 10*time.Minute, 2*time.Second, func() (bool, error) {
		h, err := celestia.Height(ctx)
		return h >= endHeight, err
	}), "chain did not reach height %d", endHeight)

	s.Require().Contains(s.containerLogs(ctx, byzantineNode.Name()), "running malicious app",
		"the byzantine node does not run the malicious app")

	byzantineBlocksCommitted := s.requireValidDataRoots(ctx, honestNodes[0], byzantineAddr, endHeight)
	if behavior.everyProposalInvalid {
		s.Require().Zero(byzantineBlocksCommitted,
			"honest validators committed blocks proposed by the byzantine validator")
	}

	rejections := 0
	for _, node := range honestNodes {
		for line := range strings.Lines(s.containerLogs(ctx, node.Name())) {
			if strings.Contains(line, rejectedProposalLog) && strings.Contains(line, "data root") {
				rejections++
			}
		}
	}
	t.Logf("honest validators logged %d proposals for a bad data root", rejections)
	s.Require().Positive(rejections, "honest validators rejected no proposal for a bad data root")

	s.requireAgreement(ctx, honestNodes, endHeight)
}

// requireValidDataRoots checks that the data root of every block from
// maliciousStartHeight to endHeight is the one computed from the block's txs.
// It returns how many of those blocks the byzantine validator proposed.
func (s *CelestiaTestSuite) requireValidDataRoots(ctx context.Context, node *cosmos.ChainNode, byzantineAddr []byte, endHeight int64) int {
	byzantine := 0
	for height := int64(maliciousStartHeight); height <= endHeight; height++ {
		block, err := node.Client.Block(ctx, &height)
		s.Require().NoError(err, "failed to get block %d", height)
		header := block.Block.Header

		eds, err := da.ConstructEDS(block.Block.Txs.ToSliceOfBytes(), header.Version.App, -1)
		s.Require().NoError(err, "failed to construct the square of block %d", height)
		dah, err := da.NewDataAvailabilityHeader(eds)
		s.Require().NoError(err)
		s.Require().Equal(dah.Hash(), []byte(header.DataHash),
			"block %d proposed by %s has an invalid data root", height, header.ProposerAddress)

		if bytes.Equal(header.ProposerAddress, byzantineAddr) {
			byzantine++
		}
	}
	s.T().Logf("byzantine validator proposed %d of the committed blocks %d to %d", byzantine, maliciousStartHeight, endHeight)
	return byzantine
}

func maliciousImage() string {
	if image := os.Getenv(maliciousImageEnvVar); image != "" {
		return image
	}
	return defaultMaliciousImage
}

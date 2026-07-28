package e2e_test

import (
	"context"
	"crypto/rand"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	"github.com/celestiaorg/celestia-app/v10/test/util/testnode"
	"github.com/celestiaorg/go-square/v4/share"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// FibrePruningTestSuite covers shard pruning.
type FibrePruningTestSuite struct {
	suite.Suite

	cctx        testnode.Context
	fibreServer *fibre.Server
	fibreClient *fibre.Client
	txClient    *user.TxClient
}

func TestFibrePruningTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping fibre pruning e2e test in short mode")
	}
	suite.Run(t, new(FibrePruningTestSuite))
}

func (s *FibrePruningTestSuite) SetupSuite() {
	t := s.T()

	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	cfg := testnode.DefaultConfig().
		WithFundedAccounts(fibre.DefaultKeyName).
		WithDelayedPrecommitTimeout(500 * time.Millisecond).
		WithModifiers(setFibreShortLifetimes(ecfg.Codec, 5*time.Second, 5*time.Second))

	cctx, _, grpcAddr := testnode.NewNetwork(t, cfg)
	s.cctx = cctx
	_, err := s.cctx.WaitForHeight(1)
	require.NoError(t, err)

	stack := startFibreStack(t, cctx, ecfg, grpcAddr)
	s.fibreServer, s.fibreClient, s.txClient = stack.server, stack.client, stack.tx

	fundEscrow(t, cctx, s.txClient, sdk.NewCoin(appconsts.BondDenom, sdkmath.NewInt(50_000_000)))
}

func (s *FibrePruningTestSuite) TearDownSuite() {
	if s.fibreServer != nil {
		_ = s.fibreServer.Stop(context.Background())
	}
	if s.fibreClient != nil {
		_ = s.fibreClient.Stop(context.Background())
	}
}

func (s *FibrePruningTestSuite) TestPruneExpiredShard() {
	t := s.T()
	ctx := s.cctx.GoContext()

	require.NoError(t, s.cctx.WaitForNextBlock())

	data := make([]byte, 4*1024)
	_, err := rand.Read(data)
	require.NoError(t, err)

	blob, err := fibre.NewBlob(data, fibre.DefaultBlobConfigV0())
	require.NoError(t, err)
	defer blob.Free()

	ns := share.MustNewV0Namespace([]byte{0x9A, 0x9B})
	_, err = s.fibreClient.Upload(ctx, ns, blob, fibre.WithKeyName(s.txClient.DefaultAccountName()))
	require.NoError(t, err)

	downloaded, err := s.fibreClient.Download(ctx, blob.ID())
	require.NoError(t, err)
	defer downloaded.Free()
	require.Equal(t, data, downloaded.Data())

	pruned, err := s.fibreServer.Store().PruneBefore(ctx, time.Now())
	require.NoError(t, err)
	require.Zero(t, pruned, "shard must not be pruned before its retention deadline")

	pruned, err = s.fibreServer.Store().PruneBefore(ctx, time.Now().Add(2*time.Minute))
	require.NoError(t, err)
	require.Equal(t, 1, pruned, "the expired shard should be pruned")

	_, err = s.fibreClient.Download(ctx, blob.ID())
	require.ErrorIs(t, err, fibre.ErrNotFound)
}

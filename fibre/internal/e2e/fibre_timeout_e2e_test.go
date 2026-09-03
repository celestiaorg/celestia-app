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
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
	"github.com/stretchr/testify/suite"
)

// promiseTimeout is shortened so an unsettled promise expires within the test.
const promiseTimeout = 5 * time.Second

// FibreTimeoutTestSuite covers the timeout settlement path
type FibreTimeoutTestSuite struct {
	suite.Suite

	cctx        testnode.Context
	fibreServer *fibre.Server
	fibreClient *fibre.Client
	txClient    *user.TxClient
}

func TestFibreTimeoutTestSuite(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping fibre timeout e2e test in short mode")
	}
	suite.Run(t, new(FibreTimeoutTestSuite))
}

func (s *FibreTimeoutTestSuite) SetupSuite() {
	t := s.T()

	ecfg := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	cfg := testnode.DefaultConfig().
		WithFundedAccounts(fibre.DefaultKeyName).
		WithDelayedPrecommitTimeout(500 * time.Millisecond).
		WithModifiers(setFibreShortLifetimes(ecfg.Codec, promiseTimeout, promiseTimeout))

	cctx, _, grpcAddr := testnode.NewNetwork(t, cfg)
	s.cctx = cctx
	_, err := s.cctx.WaitForHeight(1)
	require.NoError(t, err)

	stack := startFibreStack(t, cctx, ecfg, grpcAddr)
	s.fibreServer, s.fibreClient, s.txClient = stack.server, stack.client, stack.tx

	fundEscrow(t, cctx, s.txClient, sdk.NewCoin(appconsts.BondDenom, sdkmath.NewInt(50_000_000)))
}

func (s *FibreTimeoutTestSuite) TearDownSuite() {
	if s.fibreServer != nil {
		_ = s.fibreServer.Stop(context.Background())
	}
	if s.fibreClient != nil {
		_ = s.fibreClient.Stop(context.Background())
	}
}

// TestTimeoutSettlement uploads a blob but never submits its PayForFibre
func (s *FibreTimeoutTestSuite) TestTimeoutSettlement() {
	t := s.T()
	ctx := s.cctx.GoContext()

	require.NoError(t, s.cctx.WaitForNextBlock())

	data := make([]byte, 4*1024)
	_, err := rand.Read(data)
	require.NoError(t, err)

	blob, err := fibre.NewBlob(data, fibre.DefaultBlobConfigV0())
	require.NoError(t, err)
	defer blob.Free()

	ns := share.MustNewV0Namespace([]byte{0x7A, 0x7B})
	signed, err := s.fibreClient.Upload(ctx, ns, blob, fibre.WithKeyName(s.txClient.DefaultAccountName()))
	require.NoError(t, err)

	promiseProto, err := signed.ToProto()
	require.NoError(t, err)

	timeoutMsg := &fibretypes.MsgPaymentPromiseTimeout{
		Signer:         s.txClient.DefaultAddress().String(),
		PaymentPromise: *promiseProto,
	}
	submit := func() error {
		_, err := s.txClient.SubmitTx(ctx, []sdk.Msg{timeoutMsg},
			user.SetGasLimit(300_000), user.SetFee(5_000))
		return err
	}

	fibreQ := fibretypes.NewQueryClient(s.cctx.GRPCClient)
	signer := s.txClient.DefaultAddress().String()
	escrowBalance := func() sdk.Coin {
		resp, err := fibreQ.EscrowAccount(ctx, &fibretypes.QueryEscrowAccountRequest{Signer: signer})
		require.NoError(t, err)
		require.True(t, resp.Found)
		return resp.EscrowAccount.Balance
	}

	before := escrowBalance()

	err = submit()
	require.Error(t, err, "timeout settlement must be refused before the promise expires")
	require.ErrorContains(t, err, "timed out")

	expiration := promiseProto.CreationTimestamp.Add(promiseTimeout)
	for time.Now().Before(expiration) {
		require.NoError(t, s.cctx.WaitForNextBlock())
	}
	require.NoError(t, s.cctx.WaitForNextBlock())
	require.NoError(t, submit(), "timeout settlement should succeed once the promise has expired")

	hash, err := signed.Hash()
	require.NoError(t, err)
	waitForPaymentProcessed(t, s.cctx, hash)

	uploadSize := uint32(fibre.DefaultBlobConfigV0().UploadSize(len(data)))
	wantDebit := fibretypes.PaymentAmount(uploadSize)
	require.Equal(t, wantDebit, before.Sub(escrowBalance()),
		"escrow should be debited by the payment amount on timeout settlement")
}

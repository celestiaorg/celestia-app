package app_test

import (
	"bytes"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibrekeeper "github.com/celestiaorg/celestia-app/v11/x/fibre/keeper"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
)

// TestFibreSettlementRoutesToFeeCollector is an app-level test exercising the
// real bank and fibre keepers. Both PFF and timeout settlement must charge
// the native v11 price and move those coins into the fee collector. It also
// verifies that the module-account invariant
// (bank balance == sum of escrow balances) holds across the settlement.
func TestFibreSettlementRoutesToFeeCollector(t *testing.T) {
	for _, timeout := range []bool{false, true} {
		t.Run(fmt.Sprintf("timeout=%t", timeout), func(t *testing.T) {
			testFibreSettlementRoutesToFeeCollector(t, timeout)
		})
	}
}

func testFibreSettlementRoutesToFeeCollector(t *testing.T, timeout bool) {
	originalHome := app.NodeHome
	app.NodeHome = t.TempDir()
	t.Cleanup(func() { app.NodeHome = originalHome })
	funder := testfactory.GenerateAccounts(1)[0]
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), funder)

	ctx := testApp.NewContext(true).
		WithBlockTime(time.Date(2026, 6, 23, 12, 0, 0, 0, time.UTC)).
		WithChainID("test-chain").WithBlockHeight(100)

	moduleAddr := testApp.AccountKeeper.GetModuleAddress(fibretypes.ModuleName)
	feeCollectorAddr := testApp.AccountKeeper.GetModuleAddress(authtypes.FeeCollectorName)

	// Escrow owner: a fresh key that signs the payment promise. It does not need
	// on-chain funds because we seed the escrow state and module balance directly.
	ownerPriv := secp256k1.GenPrivKey()
	ownerPub := ownerPriv.PubKey().(*secp256k1.PubKey)
	ownerAddr := sdk.AccAddress(ownerPub.Address()).String()

	// Seed the post-deposit state: move real coins into the fibre module account
	// (as DepositToEscrow would) and record a matching escrow balance.
	deposit := sdk.NewInt64Coin(appconsts.BondDenom, 5_000_000)
	funderAddr := testfactory.GetAddress(kr, funder)
	require.NoError(t, testApp.BankKeeper.SendCoinsFromAccountToModule(ctx, funderAddr, fibretypes.ModuleName, sdk.NewCoins(deposit)))
	testApp.FibreKeeper.SetEscrowAccount(ctx, fibretypes.EscrowAccount{
		Signer:           ownerAddr,
		Balance:          deposit,
		AvailableBalance: deposit,
	})

	// Pre-condition: invariant holds (module balance == escrow balance).
	require.Equal(t, deposit.Amount, testApp.BankKeeper.GetBalance(ctx, moduleAddr, appconsts.BondDenom).Amount)

	// Build an owner-signed payment promise; only the timeout path needs it expired.
	params := testApp.FibreKeeper.GetParams(ctx)
	creation := ctx.BlockTime().Add(-time.Second)
	if timeout {
		creation = ctx.BlockTime().Add(-params.PaymentPromiseTimeout).Add(-time.Hour)
	}
	// Height must be positive for stateless validation; the timeout path does
	// not enforce the height window, so any positive height works.
	promise := buildSignedPromise(t, 99, creation, *ownerPub, ownerPriv)

	// Pin the actual charge independently of the helper used by settlement:
	// one chunk costs 695,000 gas at 0.004 utia per gas in native v11.
	payment := sdk.NewInt64Coin(appconsts.BondDenom, 2_780)
	feeBefore := testApp.BankKeeper.GetBalance(ctx, feeCollectorAddr, appconsts.BondDenom)

	// Both settlement paths must debit the same revised price.
	msgServer := fibrekeeper.NewMsgServerImpl(*testApp.FibreKeeper)
	var err error
	if timeout {
		_, err = msgServer.PaymentPromiseTimeout(ctx, &fibretypes.MsgPaymentPromiseTimeout{Signer: ownerAddr, PaymentPromise: promise})
	} else {
		_, err = msgServer.PayForFibre(ctx, &fibretypes.MsgPayForFibre{Signer: ownerAddr, PaymentPromise: promise})
	}
	require.NoError(t, err)

	moduleAfter := testApp.BankKeeper.GetBalance(ctx, moduleAddr, appconsts.BondDenom)
	feeAfter := testApp.BankKeeper.GetBalance(ctx, feeCollectorAddr, appconsts.BondDenom)
	escrow, found := testApp.FibreKeeper.GetEscrowAccount(ctx, ownerAddr)
	require.True(t, found)
	require.Equal(t, deposit.Sub(payment), escrow.Balance)
	require.Equal(t, deposit.Sub(payment), escrow.AvailableBalance)

	// The payment left the module account and landed in the fee collector.
	require.Equal(t, deposit.Amount.Sub(payment.Amount), moduleAfter.Amount, "module account should shrink by the payment")
	require.Equal(t, feeBefore.Amount.Add(payment.Amount), feeAfter.Amount, "fee collector should grow by the payment")
	// Invariant restored: module-account balance equals the sum of escrow balances.
	require.Equal(t, escrow.Balance.Amount, moduleAfter.Amount, "module balance must equal escrow balance after settlement")
}

// buildSignedPromise constructs a PaymentPromise signed by the escrow owner,
// mirroring the construction used by the keeper unit tests.
func buildSignedPromise(t *testing.T, height int64, creation time.Time, ownerPub secp256k1.PubKey, ownerPriv *secp256k1.PrivKey) fibretypes.PaymentPromise {
	t.Helper()
	promise := fibretypes.PaymentPromise{
		ChainId:           "test-chain",
		Height:            height,
		Namespace:         share.MustNewV0Namespace(bytes.Repeat([]byte{0x1}, share.NamespaceVersionZeroIDSize)).Bytes(),
		BlobSize:          1000,
		BlobVersion:       0,
		Commitment:        make([]byte, 32),
		CreationTimestamp: creation,
		SignerPublicKey:   ownerPub,
		Signature:         make([]byte, 64),
	}

	pp := fibre.PaymentPromise{}
	require.NoError(t, pp.FromProto(&promise))
	signBytes, err := pp.SignBytes()
	require.NoError(t, err)
	signature, err := ownerPriv.Sign(signBytes)
	require.NoError(t, err)
	promise.Signature = signature
	return promise
}

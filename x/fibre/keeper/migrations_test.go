package keeper_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	storetypes "cosmossdk.io/store/types"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/x/fibre"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/keeper"
	"github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/codec"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	"github.com/stretchr/testify/require"
)

// refund records one SendCoinsFromModuleToAccount call made by the migration.
type refund struct {
	module string
	to     sdk.AccAddress
	amount sdk.Coins
}

type migrationFixture struct {
	ctx     sdk.Context
	keeper  *keeper.Keeper
	bank    *MockBankKeeper
	refunds []refund
}

func newMigrationFixture(t *testing.T) *migrationFixture {
	t.Helper()
	storeKey := storetypes.NewKVStoreKey(types.StoreKey)
	db := dbm.NewMemDB()
	stateStore := store.NewCommitMultiStore(db, log.NewNopLogger(), metrics.NewNoOpMetrics())
	stateStore.MountStoreWithDB(storeKey, storetypes.StoreTypeIAVL, db)
	require.NoError(t, stateStore.LoadLatestVersion())

	cdc := codec.NewProtoCodec(codectypes.NewInterfaceRegistry())
	f := &migrationFixture{bank: &MockBankKeeper{}}
	f.bank.SendCoinsFromModuleToAccountFn = func(_ context.Context, senderModule string, recipientAddr sdk.AccAddress, amt sdk.Coins) error {
		f.refunds = append(f.refunds, refund{module: senderModule, to: recipientAddr, amount: amt})
		return nil
	}
	authority := authtypes.NewModuleAddress("gov").String()
	f.ctx = sdk.NewContext(stateStore, cmtproto.Header{ChainID: "test-chain", Time: time.Unix(1_700_000_000, 0).UTC(), Height: 100}, false, log.NewNopLogger())
	f.keeper = keeper.NewKeeper(cdc, storeKey, f.bank, &MockStakingKeeper{}, authority, false, nil)
	f.keeper.SetParams(f.ctx, types.DefaultParams())
	return f
}

func (f *migrationFixture) migrate(t *testing.T) {
	t.Helper()
	require.NoError(t, keeper.NewMigrator(*f.keeper).Migrate1to2(f.ctx))
}

func (f *migrationFixture) escrowAccounts() []types.EscrowAccount {
	var accounts []types.EscrowAccount
	f.keeper.IterateEscrowAccounts(f.ctx, func(account types.EscrowAccount) bool {
		accounts = append(accounts, account)
		return false
	})
	return accounts
}

func (f *migrationFixture) withdrawals() []types.Withdrawal {
	var withdrawals []types.Withdrawal
	f.keeper.IterateWithdrawals(f.ctx, func(withdrawal types.Withdrawal) bool {
		withdrawals = append(withdrawals, withdrawal)
		return false
	})
	return withdrawals
}

// withdrawalsAvailableBy reads the secondary index, which is what BeginBlocker
// pays withdrawals from.
func (f *migrationFixture) withdrawalsAvailableBy(t *testing.T, upTo time.Time) []types.Withdrawal {
	t.Helper()
	iterator := f.keeper.GetWithdrawalsByAvailableIterator(f.ctx, upTo)
	defer iterator.Close()
	var withdrawals []types.Withdrawal
	for ; iterator.Valid(); iterator.Next() {
		_, _, signer, err := f.keeper.ParseWithdrawalsByAvailableKey(iterator.Key())
		require.NoError(t, err)
		var withdrawal types.Withdrawal
		require.NoError(t, withdrawal.Unmarshal(iterator.Value()))
		require.Equal(t, signer, withdrawal.Signer)
		withdrawals = append(withdrawals, withdrawal)
	}
	return withdrawals
}

func newSigner() (canonical, upper string) {
	canonical = sdk.AccAddress(secp256k1.GenPrivKey().PubKey().Address()).String()
	return canonical, strings.ToUpper(canonical)
}

func utia(amount int64) sdk.Coin { return sdk.NewInt64Coin(appconsts.BondDenom, amount) }

func TestMigrate1to2EmptyStore(t *testing.T) {
	f := newMigrationFixture(t)
	f.migrate(t)
	require.Empty(t, f.escrowAccounts())
	require.Empty(t, f.withdrawals())
	require.Empty(t, f.refunds)
}

func TestMigrate1to2CanonicalUtiaAccountUntouched(t *testing.T) {
	f := newMigrationFixture(t)
	signer, _ := newSigner()
	account := types.EscrowAccount{Signer: signer, Balance: utia(1000), AvailableBalance: utia(600)}
	f.keeper.SetEscrowAccount(f.ctx, account)
	withdrawal := types.Withdrawal{Signer: signer, Amount: utia(400), RequestedTimestamp: f.ctx.BlockTime(), AvailableTimestamp: f.ctx.BlockTime().Add(time.Hour)}
	f.keeper.SetWithdrawal(f.ctx, withdrawal)

	f.migrate(t)

	require.Equal(t, []types.EscrowAccount{account}, f.escrowAccounts())
	require.Equal(t, []types.Withdrawal{withdrawal}, f.withdrawals())
	require.Equal(t, []types.Withdrawal{withdrawal}, f.withdrawalsAvailableBy(t, withdrawal.AvailableTimestamp))
	require.Empty(t, f.refunds)
}

func TestMigrate1to2MergesIntoFreshCanonicalRecord(t *testing.T) {
	f := newMigrationFixture(t)
	canonical, upper := newSigner()
	f.keeper.SetEscrowAccount(f.ctx, types.EscrowAccount{Signer: upper, Balance: utia(1000), AvailableBalance: utia(700)})
	withdrawal := types.Withdrawal{Signer: upper, Amount: utia(300), RequestedTimestamp: f.ctx.BlockTime(), AvailableTimestamp: f.ctx.BlockTime().Add(time.Hour)}
	f.keeper.SetWithdrawal(f.ctx, withdrawal)

	f.migrate(t)

	_, found := f.keeper.GetEscrowAccount(f.ctx, upper)
	require.False(t, found)
	require.Empty(t, f.keeper.GetWithdrawalsBySigner(f.ctx, upper))

	want := types.EscrowAccount{Signer: canonical, Balance: utia(1000), AvailableBalance: utia(700)}
	require.Equal(t, []types.EscrowAccount{want}, f.escrowAccounts())

	withdrawal.Signer = canonical
	require.Equal(t, []types.Withdrawal{withdrawal}, f.withdrawals())
	require.Equal(t, []types.Withdrawal{withdrawal}, f.withdrawalsAvailableBy(t, withdrawal.AvailableTimestamp))
	require.Empty(t, f.refunds)
}

func TestMigrate1to2MergesIntoExistingCanonicalRecord(t *testing.T) {
	f := newMigrationFixture(t)
	canonical, upper := newSigner()
	f.keeper.SetEscrowAccount(f.ctx, types.EscrowAccount{Signer: canonical, Balance: utia(500), AvailableBalance: utia(200)})
	f.keeper.SetEscrowAccount(f.ctx, types.EscrowAccount{Signer: upper, Balance: utia(1000), AvailableBalance: utia(400)})

	t0 := f.ctx.BlockTime()
	// Collides with the canonical withdrawal requested at t0: amounts are
	// summed and the canonical available timestamp is kept.
	canonicalW := types.Withdrawal{Signer: canonical, Amount: utia(300), RequestedTimestamp: t0, AvailableTimestamp: t0.Add(time.Hour)}
	upperW1 := types.Withdrawal{Signer: upper, Amount: utia(250), RequestedTimestamp: t0, AvailableTimestamp: t0.Add(2 * time.Hour)}
	// No collision: re-keyed as is.
	upperW2 := types.Withdrawal{Signer: upper, Amount: utia(350), RequestedTimestamp: t0.Add(time.Minute), AvailableTimestamp: t0.Add(time.Minute + time.Hour)}
	for _, w := range []types.Withdrawal{canonicalW, upperW1, upperW2} {
		f.keeper.SetWithdrawal(f.ctx, w)
	}

	f.migrate(t)

	_, found := f.keeper.GetEscrowAccount(f.ctx, upper)
	require.False(t, found)
	want := types.EscrowAccount{Signer: canonical, Balance: utia(1500), AvailableBalance: utia(600)}
	require.Equal(t, []types.EscrowAccount{want}, f.escrowAccounts())

	merged := types.Withdrawal{Signer: canonical, Amount: utia(550), RequestedTimestamp: t0, AvailableTimestamp: t0.Add(time.Hour)}
	rekeyed := upperW2
	rekeyed.Signer = canonical
	require.Equal(t, []types.Withdrawal{merged, rekeyed}, f.withdrawals())
	require.Equal(t, []types.Withdrawal{merged, rekeyed}, f.withdrawalsAvailableBy(t, t0.Add(3*time.Hour)))
	require.Empty(t, f.refunds)
}

func TestMigrate1to2RefundsNonUtiaAccount(t *testing.T) {
	f := newMigrationFixture(t)
	signer, _ := newSigner()
	other := sdk.NewInt64Coin("uatom", 1000)
	f.keeper.SetEscrowAccount(f.ctx, types.EscrowAccount{Signer: signer, Balance: other, AvailableBalance: sdk.NewInt64Coin("uatom", 600)})
	t0 := f.ctx.BlockTime()
	f.keeper.SetWithdrawal(f.ctx, types.Withdrawal{Signer: signer, Amount: sdk.NewInt64Coin("uatom", 400), RequestedTimestamp: t0, AvailableTimestamp: t0.Add(time.Hour)})

	// An untouched utia neighbour.
	neighbour, _ := newSigner()
	neighbourAccount := types.EscrowAccount{Signer: neighbour, Balance: utia(10), AvailableBalance: utia(10)}
	f.keeper.SetEscrowAccount(f.ctx, neighbourAccount)

	f.migrate(t)

	addr, err := sdk.AccAddressFromBech32(signer)
	require.NoError(t, err)
	require.Equal(t, []refund{{module: types.ModuleName, to: addr, amount: sdk.Coins{other}}}, f.refunds)
	require.Equal(t, []types.EscrowAccount{neighbourAccount}, f.escrowAccounts())
	require.Empty(t, f.withdrawals())
	require.Empty(t, f.withdrawalsAvailableBy(t, t0.Add(24*time.Hour)))
}

func TestMigrate1to2RefundsNonUtiaUppercaseAccount(t *testing.T) {
	f := newMigrationFixture(t)
	canonical, upper := newSigner()
	other := sdk.NewInt64Coin("uatom", 1000)
	f.keeper.SetEscrowAccount(f.ctx, types.EscrowAccount{Signer: upper, Balance: other, AvailableBalance: other})

	f.migrate(t)

	addr, err := sdk.AccAddressFromBech32(canonical)
	require.NoError(t, err)
	require.Equal(t, []refund{{module: types.ModuleName, to: addr, amount: sdk.Coins{other}}}, f.refunds)
	require.Empty(t, f.escrowAccounts())
}

func TestMigrate1to2RefundFailureLeavesRecord(t *testing.T) {
	f := newMigrationFixture(t)
	signer, _ := newSigner()
	other := sdk.NewInt64Coin("uatom", 1000)
	account := types.EscrowAccount{Signer: signer, Balance: other, AvailableBalance: other}
	f.keeper.SetEscrowAccount(f.ctx, account)
	t0 := f.ctx.BlockTime()
	withdrawal := types.Withdrawal{Signer: signer, Amount: sdk.NewInt64Coin("uatom", 400), RequestedTimestamp: t0, AvailableTimestamp: t0.Add(time.Hour)}
	f.keeper.SetWithdrawal(f.ctx, withdrawal)
	f.bank.SendCoinsFromModuleToAccountFn = func(context.Context, string, sdk.AccAddress, sdk.Coins) error {
		return errors.New("blocked address")
	}

	f.migrate(t)

	require.Equal(t, []types.EscrowAccount{account}, f.escrowAccounts())
	require.Equal(t, []types.Withdrawal{withdrawal}, f.withdrawals())
}

func TestMigrate1to2ZeroBalanceNonUtiaAccountDeletedWithoutRefund(t *testing.T) {
	f := newMigrationFixture(t)
	signer, _ := newSigner()
	zero := sdk.NewInt64Coin("uatom", 0)
	f.keeper.SetEscrowAccount(f.ctx, types.EscrowAccount{Signer: signer, Balance: zero, AvailableBalance: zero})

	f.migrate(t)

	require.Empty(t, f.refunds)
	require.Empty(t, f.escrowAccounts())
}

func TestMigrate1to2UnparseableSignerLeftAlone(t *testing.T) {
	f := newMigrationFixture(t)
	for _, account := range []types.EscrowAccount{
		{Signer: "not-an-address", Balance: utia(1000), AvailableBalance: utia(1000)},
		{Signer: "not-an-address-either", Balance: sdk.NewInt64Coin("uatom", 1000), AvailableBalance: sdk.NewInt64Coin("uatom", 1000)},
	} {
		f.keeper.SetEscrowAccount(f.ctx, account)
	}
	before := f.escrowAccounts()
	require.Len(t, before, 2)

	f.migrate(t)

	require.Equal(t, before, f.escrowAccounts())
	require.Empty(t, f.refunds)
}

func TestMigrate1to2DenomMismatchWithCanonicalLeftAlone(t *testing.T) {
	f := newMigrationFixture(t)
	canonical, upper := newSigner()
	other := sdk.NewInt64Coin("uatom", 1000)
	canonicalAccount := types.EscrowAccount{Signer: canonical, Balance: other, AvailableBalance: other}
	upperAccount := types.EscrowAccount{Signer: upper, Balance: utia(500), AvailableBalance: utia(500)}
	f.keeper.SetEscrowAccount(f.ctx, canonicalAccount)
	f.keeper.SetEscrowAccount(f.ctx, upperAccount)
	// The canonical record's refund fails, so it stays non-utia and the
	// uppercase utia record must not be folded into it.
	f.bank.SendCoinsFromModuleToAccountFn = func(context.Context, string, sdk.AccAddress, sdk.Coins) error {
		return errors.New("blocked address")
	}

	f.migrate(t)

	got := f.escrowAccounts()
	require.ElementsMatch(t, []types.EscrowAccount{canonicalAccount, upperAccount}, got)
}

func TestMigrate1to2IsIdempotent(t *testing.T) {
	f := newMigrationFixture(t)
	canonical, upper := newSigner()
	f.keeper.SetEscrowAccount(f.ctx, types.EscrowAccount{Signer: upper, Balance: utia(1000), AvailableBalance: utia(1000)})
	other, _ := newSigner()
	f.keeper.SetEscrowAccount(f.ctx, types.EscrowAccount{Signer: other, Balance: sdk.NewInt64Coin("uatom", 7), AvailableBalance: sdk.NewInt64Coin("uatom", 7)})

	f.migrate(t)
	after := f.escrowAccounts()
	refunds := len(f.refunds)
	f.migrate(t)

	require.Equal(t, []types.EscrowAccount{{Signer: canonical, Balance: utia(1000), AvailableBalance: utia(1000)}}, after)
	require.Equal(t, after, f.escrowAccounts())
	require.Len(t, f.refunds, refunds)
}

func TestFibreConsensusVersion(t *testing.T) {
	require.Equal(t, uint64(2), fibre.AppModule{}.ConsensusVersion())
}

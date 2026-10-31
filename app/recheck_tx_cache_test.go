package app_test

import (
	"testing"
	"time"

	"cosmossdk.io/math"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/fibre"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/blobfactory"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	minfeetypes "github.com/celestiaorg/celestia-app/v11/x/minfee/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// TestRecheckPayForFibreMatchesBaseApp rechecks the same pending PayForFibre
// txs on two identical apps, through the cached decode on one and through
// BaseApp.CheckTx, the path main takes, on the other. Every state change that
// can evict a PFF is applied in turn; responses and check state must match.
func TestRecheckPayForFibreMatchesBaseApp(t *testing.T) {
	const (
		control = iota
		staleSeq
		feeDrained
		replayed
		escrowDrained
		timeoutHeight
		neverAdmitted
		numAccounts
	)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(numAccounts)
	genesis, valSet, kr := testutil.GenesisStateWithSingleValidator(testutil.NewTestApp(), accounts...)
	newApp := func() *app.App {
		a := testutil.InitialiseTestAppWithGenesis(testutil.NewTestApp(), app.DefaultConsensusParams(), genesis)
		_, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{
			Time: testutil.GenesisTime, Height: 1, Hash: a.LastCommitID().Hash, NextValidatorsHash: valSet.Hash(),
		})
		require.NoError(t, err)
		_, err = a.Commit()
		require.NoError(t, err)
		return a
	}
	lite, ref := newApp(), newApp()
	both := func(fn func(a *app.App)) { fn(lite); fn(ref) }

	now := time.Now().Truncate(time.Second)
	addrs := make([]sdk.AccAddress, numAccounts)
	newSigner := func(i int) *user.Signer {
		acc := testutil.DirectQueryAccount(lite, testfactory.GetAddress(kr, accounts[i]))
		s, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, user.NewAccount(accounts[i], acc.GetAccountNumber(), acc.GetSequence()))
		require.NoError(t, err)
		return s
	}
	signers := make([]*user.Signer, numAccounts)
	for i := range accounts {
		addrs[i] = testfactory.GetAddress(kr, accounts[i])
		signers[i] = newSigner(i)
		both(func(a *app.App) { seedEscrow(t, a, addrs[i], 1_000_000, now) })
	}
	both(func(a *app.App) { commitAt(t, a, now) })

	pff := func(i int, created time.Time, opts ...user.TxOption) []byte {
		opts = append([]user.TxOption{user.SetGasLimit(1_000_000), user.SetFee(4_000)}, opts...)
		raw := signedPFF(t, signers[i], accounts[i], created, opts...)
		require.NoError(t, signers[i].IncrementSequence(accounts[i]))
		return raw
	}
	sendTx := func(i int, amount int64) []byte {
		msg := banktypes.NewMsgSend(addrs[i], addrs[control], sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, amount)))
		raw, _, err := newSigner(i).CreateTx([]sdk.Msg{msg}, user.SetGasLimit(200_000), user.SetFee(4_000))
		require.NoError(t, err)
		return raw
	}

	pending := [][]byte{
		pff(control, now),
		pff(staleSeq, now), pff(staleSeq, now.Add(-time.Second)),
		pff(feeDrained, now),
		pff(replayed, now), pff(replayed, now), // one promise in two envelopes
		pff(escrowDrained, now),
		pff(timeoutHeight, now, user.SetTimeoutHeight(uint64(lite.LastBlockHeight()+3))),
		pff(neverAdmitted, now),
	}
	staleTx, replayTx := pending[1], pending[5]
	settledOriginal := pending[4]
	// Bumps staleSeq's sequence in a block, so its first pending tx goes stale.
	blockSend := sendTx(staleSeq, 1)

	code := func(raw []byte) uint32 {
		res, err := ref.BaseApp.CheckTx(&abci.RequestCheckTx{Tx: raw, Type: abci.CheckTxType_Recheck})
		require.NoError(t, err)
		return res.Code
	}
	for _, raw := range pending[:len(pending)-1] {
		resLite, err := lite.CheckTx(&abci.RequestCheckTx{Tx: raw, Type: abci.CheckTxType_New})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, resLite.Code, resLite.Log)
		resRef, err := ref.CheckTx(&abci.RequestCheckTx{Tx: raw, Type: abci.CheckTxType_New})
		require.NoError(t, err)
		require.Equal(t, resRef, resLite)
		require.True(t, lite.RecheckTxCached(raw))
	}
	require.False(t, lite.RecheckTxCached(pending[len(pending)-1]))

	// Only admitted PayForFibres are kept: not a rejected one, nor another type.
	rejected := pff(control, now, user.SetFee(1))
	plain := sendTx(neverAdmitted, 1)
	for _, raw := range [][]byte{rejected, plain} {
		resLite, err := lite.CheckTx(&abci.RequestCheckTx{Tx: raw, Type: abci.CheckTxType_New})
		require.NoError(t, err)
		resRef, err := ref.CheckTx(&abci.RequestCheckTx{Tx: raw, Type: abci.CheckTxType_New})
		require.NoError(t, err)
		require.Equal(t, resRef, resLite)
		require.False(t, lite.RecheckTxCached(raw))
	}
	require.NotEqual(t, abci.CodeTypeOK, code(rejected))

	recheckAll := func(phase string) {
		for i, raw := range pending {
			req := &abci.RequestCheckTx{Tx: raw, Type: abci.CheckTxType_Recheck}
			resLite, errLite := lite.CheckTx(req)
			resRef, errRef := ref.BaseApp.CheckTx(req)
			require.Equal(t, errRef, errLite, "%s: tx %d", phase, i)
			require.Equal(t, resRef, resLite, "%s: tx %d", phase, i)
		}
		requireSameCheckState(t, lite, ref, append(addrs, authtypes.NewModuleAddress(authtypes.FeeCollectorName)), phase)
	}

	both(func(a *app.App) { commitAt(t, a, now.Add(time.Second)) })
	recheckAll("unchanged state")

	// A block settles one envelope of the replayed promise and bumps staleSeq;
	// feeDrained spends its balance and escrowDrained's escrow is emptied.
	both(func(a *app.App) {
		mutate(a, now, func(ctx sdk.Context) {
			spendable := a.BankKeeper.SpendableCoins(ctx, addrs[feeDrained])
			require.NoError(t, a.BankKeeper.SendCoins(ctx, addrs[feeDrained], addrs[control], spendable))
			escrow, found := a.FibreKeeper.GetEscrowAccount(ctx, addrs[escrowDrained].String())
			require.True(t, found)
			escrow.Balance.Amount, escrow.AvailableBalance.Amount = math.ZeroInt(), math.ZeroInt()
			a.FibreKeeper.SetEscrowAccount(ctx, escrow)
		})
		commitAt(t, a, now.Add(2*time.Second), blockSend, settledOriginal)
	})
	recheckAll("block and balance changes")
	for _, raw := range [][]byte{staleTx, pending[3], replayTx, pending[6]} {
		require.NotEqual(t, abci.CodeTypeOK, code(raw))
		require.False(t, lite.RecheckTxCached(raw), "a failed recheck drops its entry")
	}
	require.True(t, lite.RecheckTxCached(pending[0]))

	both(func(a *app.App) {
		commitAt(t, a, now.Add(3*time.Second))
		commitAt(t, a, now.Add(4*time.Second))
	})
	recheckAll("timeout height passed")
	require.NotEqual(t, abci.CodeTypeOK, code(pending[7]))

	both(func(a *app.App) {
		mutate(a, now, func(ctx sdk.Context) {
			a.MinFeeKeeper.SetParams(ctx, minfeetypes.Params{NetworkMinGasPrice: math.LegacyOneDec()})
		})
		commitAt(t, a, now.Add(5*time.Second))
	})
	recheckAll("min gas price raised")
	require.NotEqual(t, abci.CodeTypeOK, code(pending[0]))

	both(func(a *app.App) {
		mutate(a, now, func(ctx sdk.Context) {
			a.MinFeeKeeper.SetParams(ctx, minfeetypes.DefaultParams())
			require.NoError(t, a.CircuitKeeper.DisableList.Set(ctx, fibretypes.MsgPayForFibreTypeURL))
		})
		commitAt(t, a, now.Add(6*time.Second))
	})
	recheckAll("circuit breaker")

	both(func(a *app.App) {
		mutate(a, now, func(ctx sdk.Context) {
			require.NoError(t, a.CircuitKeeper.DisableList.Remove(ctx, fibretypes.MsgPayForFibreTypeURL))
		})
		commitAt(t, a, now.Add(fibretypes.DefaultPaymentPromiseTimeout+time.Minute))
	})
	recheckAll("promise expired")
	require.NotEqual(t, abci.CodeTypeOK, code(pending[0]))
}

// requireSameCheckState compares the sequences and balances recheck wrote.
func requireSameCheckState(t *testing.T, a, b *app.App, addrs []sdk.AccAddress, phase string) {
	t.Helper()
	ctxA, ok := a.CheckState()
	require.True(t, ok)
	ctxB, ok := b.CheckState()
	require.True(t, ok)
	for _, addr := range addrs {
		accA, accB := a.AccountKeeper.GetAccount(ctxA, addr), b.AccountKeeper.GetAccount(ctxB, addr)
		require.NotNil(t, accA)
		require.NotNil(t, accB)
		require.Equal(t, accB.GetSequence(), accA.GetSequence(), "%s: %s", phase, addr)
		require.Equal(t, b.BankKeeper.GetAllBalances(ctxB, addr), a.BankKeeper.GetAllBalances(ctxA, addr), "%s: %s", phase, addr)
	}
}

func signedPFF(t *testing.T, signer *user.Signer, account string, created time.Time, opts ...user.TxOption) []byte {
	t.Helper()
	raw, _, err := signer.CreateTx([]sdk.Msg{pffMsg(t, signer, account, created)}, opts...)
	require.NoError(t, err)
	return raw
}

// pffMsg returns a PayForFibre whose promise account signed.
func pffMsg(t *testing.T, signer *user.Signer, account string, created time.Time) *fibretypes.MsgPayForFibre {
	t.Helper()
	msg := blobfactory.NewMsgPayForFibre(t, signer.Account(account).PubKey().(*secp256k1.PubKey), testutil.ChainID)
	msg.PaymentPromise.CreationTimestamp = created
	pp := fibre.PaymentPromise{}
	require.NoError(t, pp.FromProto(&msg.PaymentPromise))
	signBytes, err := pp.SignBytes()
	require.NoError(t, err)
	msg.PaymentPromise.Signature, _, err = signer.Keyring().Sign(account, signBytes, signing.SignMode_SIGN_MODE_DIRECT)
	require.NoError(t, err)
	valSig, err := testutil.GenesisValidatorPrivateKey().Sign(signBytes)
	require.NoError(t, err)
	msg.ValidatorSignatures = [][]byte{valSig}
	return msg
}

func mutate(a *app.App, blockTime time.Time, fn func(ctx sdk.Context)) {
	fn(a.NewUncachedContext(false, cmtproto.Header{ChainID: testutil.ChainID, Height: a.LastBlockHeight(), Time: blockTime}))
}

func seedEscrow(t *testing.T, a *app.App, owner sdk.AccAddress, amount int64, blockTime time.Time) {
	t.Helper()
	mutate(a, blockTime, func(ctx sdk.Context) {
		coins := sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, amount))
		require.NoError(t, a.BankKeeper.SendCoinsFromAccountToModule(ctx, owner, fibretypes.ModuleName, coins))
		a.FibreKeeper.SetEscrowAccount(ctx, fibretypes.EscrowAccount{Signer: owner.String(), Balance: coins[0], AvailableBalance: coins[0]})
	})
}

func commitAt(t *testing.T, a *app.App, blockTime time.Time, txs ...[]byte) {
	t.Helper()
	res, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{
		Time: blockTime, Height: a.LastBlockHeight() + 1, Hash: a.LastCommitID().Hash, Txs: txs,
	})
	require.NoError(t, err)
	for i, r := range res.TxResults {
		require.Equal(t, abci.CodeTypeOK, r.Code, "block tx %d: %s", i, r.Log)
	}
	_, err = a.Commit()
	require.NoError(t, err)
}

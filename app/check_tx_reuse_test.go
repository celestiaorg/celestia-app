package app_test

import (
	"bytes"
	"context"
	"testing"
	"time"

	"cosmossdk.io/math"
	"cosmossdk.io/x/feegrant"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	minfeetypes "github.com/celestiaorg/celestia-app/v11/x/minfee/types"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	kmultisig "github.com/cosmos/cosmos-sdk/crypto/keys/multisig"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// TestCheckTxReuseMatchesMain runs CheckTx and recheck for every tx shape on an
// app that reuses decodes and on one that decodes afresh, as main does:
// PayForFibre plain, fee-granted and multisig-paid, bank sends (single and
// multisig) and a BlobTx, across nonce bumps, a drained balance, an expired
// fee grant and a raised min gas price.
func TestCheckTxReuseMatchesMain(t *testing.T) {
	const (
		pffPlain = iota
		pffGrantee
		granter
		sender
		blobber
		msA
		msB
		funder
		numAccounts
	)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(numAccounts)
	genesis, valSet, kr := testutil.GenesisStateWithSingleValidator(testutil.NewTestApp(), accounts...)
	newApp := func() *app.App {
		a := testutil.InitialiseTestAppWithGenesis(testutil.NewTestApp(), app.DefaultConsensusParams(), genesis)
		_, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{Time: testutil.GenesisTime, Height: 1, Hash: a.LastCommitID().Hash, NextValidatorsHash: valSet.Hash()})
		require.NoError(t, err)
		_, err = a.Commit()
		require.NoError(t, err)
		return a
	}
	reuse, ref := newApp(), newApp()
	ref.DisableTxReuse()
	both := func(fn func(a *app.App)) { fn(reuse); fn(ref) }

	now := time.Now().Truncate(time.Second)
	addrs := make([]sdk.AccAddress, numAccounts)
	pks := make([]cryptotypes.PubKey, numAccounts)
	for i := range accounts {
		addrs[i] = testfactory.GetAddress(kr, accounts[i])
		rec, err := kr.Key(accounts[i])
		require.NoError(t, err)
		pks[i], err = rec.GetPubKey()
		require.NoError(t, err)
	}
	mpk := kmultisig.NewLegacyAminoPubKey(2, []cryptotypes.PubKey{pks[msA], pks[msB]})
	maddr := sdk.AccAddress(mpk.Address())
	grantExpiry := now.Add(3 * time.Second)
	both(func(a *app.App) {
		for _, i := range []int{pffPlain, pffGrantee, msA} {
			seedEscrow(t, a, addrs[i], 1_000_000, now)
		}
		mutate(a, now, func(ctx sdk.Context) {
			require.NoError(t, a.BankKeeper.SendCoins(ctx, addrs[funder], maddr, sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 10_000_000))))
			require.NoError(t, a.FeeGrantKeeper.GrantAllowance(ctx, addrs[granter], addrs[pffGrantee], &feegrant.BasicAllowance{Expiration: &grantExpiry}))
		})
		commitAt(t, a, now)
	})

	signers := make([]*user.Signer, numAccounts)
	for i := range accounts {
		acc := testutil.DirectQueryAccount(reuse, addrs[i])
		s, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, user.NewAccount(accounts[i], acc.GetAccountNumber(), acc.GetSequence()))
		require.NoError(t, err)
		signers[i] = s
	}
	next := func(i int, raw []byte) []byte {
		require.NoError(t, signers[i].IncrementSequence(accounts[i]))
		return raw
	}
	pff := func(i int, opts ...user.TxOption) []byte {
		opts = append([]user.TxOption{user.SetGasLimit(1_000_000), user.SetFee(4_000)}, opts...)
		return next(i, signedPFF(t, signers[i], accounts[i], now, opts...))
	}
	sendMsg := func(from sdk.AccAddress, amount int64) []sdk.Msg {
		return []sdk.Msg{banktypes.NewMsgSend(from, addrs[funder], sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, amount)))}
	}
	send := func(i int, amount int64) []byte {
		raw, _, err := signers[i].CreateTx(sendMsg(addrs[i], amount), user.SetGasLimit(200_000), user.SetFee(4_000))
		require.NoError(t, err)
		return raw
	}
	blob := func(i int) []byte {
		b, err := share.NewV0Blob(share.RandomBlobNamespace(), bytes.Repeat([]byte{7}, 2000))
		require.NoError(t, err)
		raw, _, err := signers[i].CreatePayForBlobs(accounts[i], []*share.Blob{b}, user.SetGasLimit(200_000), user.SetFee(4_000))
		require.NoError(t, err)
		return next(i, raw)
	}
	msAccNum := testutil.DirectQueryAccount(reuse, maddr).GetAccountNumber()
	msTx := func(seq uint64, msgs []sdk.Msg, gas uint64) []byte {
		return multisigTx(t, enc, kr, []string{accounts[msA], accounts[msB]}, mpk, msAccNum, seq, msgs, gas)
	}
	msPFF := func(seq uint64) []byte {
		msg := pffMsg(t, signers[msA], accounts[msA], now)
		msg.Signer = maddr.String()
		return msTx(seq, []sdk.Msg{msg}, 1_000_000)
	}

	pending := [][]byte{
		pff(pffPlain), pff(pffPlain),
		pff(pffGrantee, user.SetFeeGranter(addrs[granter])), pff(pffGrantee, user.SetFeeGranter(addrs[granter])),
		next(sender, send(sender, 1)), next(sender, send(sender, 1)),
		blob(blobber), blob(blobber),
		msPFF(0), msPFF(1), msTx(2, sendMsg(maddr, 1), 200_000),
	}
	isPFF := []bool{true, true, true, true, false, false, false, false, true, true, false}
	all := append(append([]sdk.AccAddress{}, addrs...), maddr, authtypes.NewModuleAddress(authtypes.FeeCollectorName))

	for i, raw := range pending {
		req := &abci.RequestCheckTx{Tx: raw, Type: abci.CheckTxType_New}
		res, err := reuse.CheckTx(req)
		resRef, errRef := ref.CheckTx(req)
		require.Equal(t, errRef, err, "tx %d", i)
		require.Equal(t, resRef, res, "tx %d", i)
		require.Equal(t, abci.CodeTypeOK, res.Code, "tx %d: %s", i, res.Log)
		require.Equal(t, isPFF[i], reuse.RecheckTxCached(raw), "tx %d", i)
	}
	requireSameCheckState(t, reuse, ref, all, "admission")

	recheckAll := func(phase string) []uint32 {
		codes := make([]uint32, len(pending))
		for i, raw := range pending {
			req := &abci.RequestCheckTx{Tx: raw, Type: abci.CheckTxType_Recheck}
			res, err := reuse.CheckTx(req)
			resRef, errRef := ref.CheckTx(req)
			require.Equal(t, errRef, err, "%s: tx %d", phase, i)
			require.Equal(t, resRef, res, "%s: tx %d", phase, i)
			codes[i] = res.Code
		}
		requireSameCheckState(t, reuse, ref, all, phase)
		return codes
	}

	both(func(a *app.App) { commitAt(t, a, now.Add(time.Second)) })
	require.Equal(t, make([]uint32, len(pending)), recheckAll("unchanged"))

	// Block txs bump sender's and the multisig's sequences; blobber is drained.
	require.NoError(t, signers[sender].SetSequence(accounts[sender], 0))
	bumps := [][]byte{send(sender, 2), msTx(0, sendMsg(maddr, 2), 200_000)}
	both(func(a *app.App) {
		mutate(a, now, func(ctx sdk.Context) {
			require.NoError(t, a.BankKeeper.SendCoins(ctx, addrs[blobber], addrs[funder], a.BankKeeper.SpendableCoins(ctx, addrs[blobber])))
		})
		commitAt(t, a, now.Add(2*time.Second), bumps...)
	})
	codes := recheckAll("nonce bumps, drained balance")
	for _, i := range []int{4, 6, 7, 8} {
		require.NotEqual(t, abci.CodeTypeOK, codes[i], "tx %d", i)
	}

	both(func(a *app.App) { commitAt(t, a, grantExpiry.Add(time.Second)) })
	codes = recheckAll("fee grant expired")
	require.NotEqual(t, abci.CodeTypeOK, codes[2])

	both(func(a *app.App) {
		mutate(a, now, func(ctx sdk.Context) {
			a.MinFeeKeeper.SetParams(ctx, minfeetypes.Params{NetworkMinGasPrice: math.LegacyOneDec()})
		})
		commitAt(t, a, now.Add(10*time.Second))
	})
	for i, code := range recheckAll("min gas price raised") {
		require.NotEqual(t, abci.CodeTypeOK, code, "tx %d", i)
	}
}

// TestRecheckUsesKeptDecode swaps two kept decodes: the recheck then judges
// the other tx, so BaseApp was handed the kept decode.
func TestRecheckUsesKeptDecode(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(1)
	genesis, valSet, kr := testutil.GenesisStateWithSingleValidator(testutil.NewTestApp(), accounts...)
	a := testutil.InitialiseTestAppWithGenesis(testutil.NewTestApp(), app.DefaultConsensusParams(), genesis)
	_, err := a.FinalizeBlock(&abci.RequestFinalizeBlock{Time: testutil.GenesisTime, Height: 1, Hash: a.LastCommitID().Hash, NextValidatorsHash: valSet.Hash()})
	require.NoError(t, err)
	_, err = a.Commit()
	require.NoError(t, err)
	now := time.Now().Truncate(time.Second)
	addr := testfactory.GetAddress(kr, accounts[0])
	seedEscrow(t, a, addr, 1_000_000, now)
	commitAt(t, a, now)

	acc := testutil.DirectQueryAccount(a, addr)
	s, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, user.NewAccount(accounts[0], acc.GetAccountNumber(), acc.GetSequence()))
	require.NoError(t, err)
	txs := make([][]byte, 2)
	for i := range txs {
		txs[i] = signedPFF(t, s, accounts[0], now, user.SetGasLimit(1_000_000), user.SetFee(4_000))
		require.NoError(t, s.IncrementSequence(accounts[0]))
		res, err := a.CheckTx(&abci.RequestCheckTx{Tx: txs[i], Type: abci.CheckTxType_New})
		require.NoError(t, err)
		require.Equal(t, abci.CodeTypeOK, res.Code, res.Log)
	}
	commitAt(t, a, now.Add(time.Second))

	a.SwapRecheckTxs(txs[0], txs[1])
	res, err := a.CheckTx(&abci.RequestCheckTx{Tx: txs[0], Type: abci.CheckTxType_Recheck})
	require.NoError(t, err)
	require.Contains(t, res.Log, "account sequence mismatch")
}

// multisigTx signs msgs with every key of mpk in SIGN_MODE_DIRECT.
func multisigTx(t *testing.T, enc encoding.Config, kr keyring.Keyring, names []string, mpk *kmultisig.LegacyAminoPubKey, accNum, seq uint64, msgs []sdk.Msg, gas uint64) []byte {
	t.Helper()
	b := enc.TxConfig.NewTxBuilder()
	require.NoError(t, b.SetMsgs(msgs...))
	b.SetGasLimit(gas)
	b.SetFeeAmount(sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 4_000)))
	bits := cryptotypes.NewCompactBitArray(len(names))
	sigs := make([]signing.SignatureData, len(names))
	for i := range names {
		bits.SetIndex(i, true)
		sigs[i] = &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_DIRECT}
	}
	setSigs := func() {
		require.NoError(t, b.SetSignatures(signing.SignatureV2{PubKey: mpk, Sequence: seq, Data: &signing.MultiSignatureData{BitArray: bits, Signatures: sigs}}))
	}
	setSigs()
	signerData := authsigning.SignerData{Address: sdk.AccAddress(mpk.Address()).String(), ChainID: testutil.ChainID, AccountNumber: accNum, Sequence: seq, PubKey: mpk}
	signBytes, err := authsigning.GetSignBytesAdapter(context.Background(), enc.TxConfig.SignModeHandler(), signing.SignMode_SIGN_MODE_DIRECT, signerData, b.GetTx())
	require.NoError(t, err)
	for i, name := range names {
		sig, _, err := kr.Sign(name, signBytes, signing.SignMode_SIGN_MODE_DIRECT)
		require.NoError(t, err)
		sigs[i] = &signing.SingleSignatureData{SignMode: signing.SignMode_SIGN_MODE_DIRECT, Signature: sig}
	}
	setSigs()
	raw, err := enc.TxConfig.TxEncoder()(b.GetTx())
	require.NoError(t, err)
	return raw
}

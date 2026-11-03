package app_test

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"strings"
	"testing"
	"time"

	sdkmath "cosmossdk.io/math"
	"cosmossdk.io/x/feegrant"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/fuzzutil"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	abci "github.com/cometbft/cometbft/abci/types"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	"github.com/cosmos/cosmos-sdk/client"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	"github.com/cosmos/cosmos-sdk/crypto/keyring"
	cryptotypes "github.com/cosmos/cosmos-sdk/crypto/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	authsigning "github.com/cosmos/cosmos-sdk/x/auth/signing"
	authtypes "github.com/cosmos/cosmos-sdk/x/auth/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

const (
	anteHeaderRecord = 24 // tx-level fields: messages, fee, gas, memo, timeout, payer, granter, signature edit
	anteMsgRecord    = 6  // per message: kind, signer, amount
	anteMaxMsgs      = 4
	// anteFixtureBlocks bounds the empty blocks one fixture commits, keeping
	// the pay-for-fibre promise inside its validity window.
	anteFixtureBlocks = 200

	antePFFAccount     = 3 // holds the fibre escrow and signs the promise
	antePubKeyAccount  = 1 // has its public key recorded on chain
	anteGranterAccount = 2 // grants an unlimited fee allowance to account 0
)

// Signature edits. Every edit but anteSigNone, anteSigOmitPubKey and
// anteSigMode forges the tx, which must then be rejected. A signer without a
// public key is valid once the account's key is on chain (antePubKeyAccount).
const (
	anteSigNone = iota
	anteSigSeqPlus
	anteSigSeqMinus
	anteSigAccNum
	anteSigChainID
	anteSigFlipByte
	anteSigEmpty
	anteSigOtherPubKey
	anteSigOmitPubKey
	anteSigDropSig
	anteSigExtraSig
	anteSigDropSigner
	anteSigExtraSigner
	anteSigMode         // declare and sign in the chosen mode
	anteSigModeMismatch // declare the chosen mode, sign in direct mode
	anteSigOps
)

var anteSignModes = []signing.SignMode{
	signing.SignMode_SIGN_MODE_LEGACY_AMINO_JSON, signing.SignMode_SIGN_MODE_TEXTUAL,
	signing.SignMode_SIGN_MODE_DIRECT_AUX, signing.SignMode_SIGN_MODE_UNSPECIFIED, signing.SignMode(999),
}

// anteChainFixture is an app whose committed state no tx ever changes: every
// input runs CheckTx and then commits an empty block, which resets the check
// state, so balances and sequences start equal for each input.
type anteChainFixture struct {
	app      *app.App
	txConfig client.TxConfig
	kr       keyring.Keyring
	names    []string
	addrs    []sdk.AccAddress
	pubKeys  []cryptotypes.PubKey
	accNums  []uint64
	pff      *fibretypes.MsgPayForFibre
	start    int64
}

var anteChainFx *anteChainFixture

func newAnteChainFixture(t *testing.T) *anteChainFixture {
	t.Helper()
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	names := testfactory.GenerateAccounts(4)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), names...)
	commitBlockAt(t, testApp, recheckBlockTime)
	fx := &anteChainFixture{app: testApp, txConfig: enc.TxConfig, kr: kr, names: names}

	ctx := testApp.NewUncachedContext(false, cmtproto.Header{ChainID: testutil.ChainID, Height: testApp.LastBlockHeight(), Time: recheckBlockTime})
	for i, name := range names {
		addr := testfactory.GetAddress(kr, name)
		record, err := kr.Key(name)
		require.NoError(t, err)
		pubKey, err := record.GetPubKey()
		require.NoError(t, err)
		acc := testApp.AccountKeeper.GetAccount(ctx, addr)
		if i == antePubKeyAccount {
			require.NoError(t, acc.SetPubKey(pubKey))
			testApp.AccountKeeper.SetAccount(ctx, acc)
		}
		fx.addrs = append(fx.addrs, addr)
		fx.pubKeys = append(fx.pubKeys, pubKey)
		fx.accNums = append(fx.accNums, acc.GetAccountNumber())
	}
	require.NoError(t, testApp.FeeGrantKeeper.GrantAllowance(ctx, fx.addrs[anteGranterAccount], fx.addrs[0], &feegrant.BasicAllowance{}))
	seedFibreEscrow(t, testApp, fx.addrs[antePFFAccount], 1_000_000)

	signer := newSignerFactory(t, kr, enc.TxConfig, names, queryAccountInfo(testApp, names, kr))(antePFFAccount)
	pffTx := newSignedPayForFibreTxAt(t, signer, names[antePFFAccount], true, recheckBlockTime.Add(-time.Minute))
	decoded, err := enc.TxConfig.TxDecoder()(pffTx)
	require.NoError(t, err)
	fx.pff = fibretypes.PayForFibreMsg(decoded)
	require.NotNil(t, fx.pff)
	commitBlockAt(t, testApp, recheckBlockTime)
	fx.start = testApp.LastBlockHeight()
	return fx
}

// anteTx is a tx built from a script and what the oracle needs to know of it.
type anteTx struct {
	raw     []byte
	signers []int     // fixture accounts the tx must be signed by
	payer   int       // account the fee must come from
	fee     sdk.Coins // nil unless the fee is a valid coin set
	gas     uint64
	forged  bool // some signature cannot verify
	// omitsPubKey is set when a signer info carries no public key.
	omitsPubKey bool
}

// build reads a tx from script: a header record, then one record per message.
// Zero bytes select a valid default for every field.
func (fx *anteChainFixture) build(t *testing.T, script []byte) anteTx {
	t.Helper()
	ctx := fx.app.NewContext(true)
	c := fuzzutil.New(script)
	h := c.Record(anteHeaderRecord)
	n := len(fx.addrs)
	out := anteTx{payer: -1}

	msgs := make([]sdk.Msg, []int{1, 2, 3, 4, 0}[h.Intn(anteMaxMsgs+1)])
	for i := range msgs {
		r := c.Record(anteMsgRecord)
		kind, from := r.Byte()%4, r.Intn(n)
		to := fx.addrs[(from+1)%n]
		switch kind {
		case 0:
			msgs[i] = banktypes.NewMsgSend(fx.addrs[from], to, sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)))
		case 1:
			msgs[i], from = fx.pff, antePFFAccount
		case 2: // any amount, including zero
			amount := sdk.Coins{{Denom: appconsts.BondDenom, Amount: sdkmath.NewIntFromUint64(uint64(r.Uint32()))}}
			msgs[i] = &banktypes.MsgSend{FromAddress: fx.addrs[from].String(), ToAddress: to.String(), Amount: amount}
		case 3: // a denom the sender does not hold
			msgs[i] = banktypes.NewMsgSend(fx.addrs[from], fx.addrs[from], sdk.NewCoins(sdk.NewInt64Coin("foo", 1)))
		}
		out.signers = appendUnique(out.signers, from)
	}

	feeMode, denomMode, amount := h.Byte()%6, h.Byte()%3, h.Uint64()
	fee := sdk.Coins{{Denom: appconsts.BondDenom, Amount: sdkmath.NewInt(20_000)}}
	switch feeMode {
	case 1:
		fee[0].Amount = sdkmath.ZeroInt()
	case 2:
		fee = nil
	case 3:
		fee[0].Amount = sdkmath.NewIntFromUint64(amount)
	case 4:
		fee[0].Amount = sdkmath.NewIntFromUint64(amount).Mul(sdkmath.NewIntFromUint64(math.MaxUint64))
	case 5:
		fee = sdk.Coins{{Denom: "foo", Amount: sdkmath.OneInt()}, fee[0]}
	}
	if denomMode > 0 && len(fee) > 0 {
		fee[len(fee)-1].Denom = []string{"", "foo", "U!"}[denomMode]
	}
	if fee.Validate() == nil || fee.Empty() {
		out.fee = fee
	}

	gasMode, gasRaw := h.Byte()%5, h.Uint32()
	out.gas = []uint64{1_000_000, 0, 1, math.MaxUint64, uint64(gasRaw)}[gasMode]

	maxMemo := int(fx.app.AccountKeeper.GetParams(ctx).MaxMemoCharacters)
	memoLen := []int{0, maxMemo - 1, maxMemo, maxMemo + 1}[h.Intn(4)]
	height := uint64(ctx.BlockHeight())
	timeout := []uint64{0, height - 1, height, height + 1000, uint64(gasRaw)}[h.Intn(5)]

	feePayer, granter := h.Intn(n+1)-1, h.Intn(n+1)-1
	auth := txtypes.AuthInfo{Fee: &txtypes.Fee{Amount: fee, GasLimit: out.gas}}
	if feePayer >= 0 {
		auth.Fee.Payer = fx.addrs[feePayer].String()
		out.signers = appendUnique(out.signers, feePayer)
	}
	if granter >= 0 {
		auth.Fee.Granter = fx.addrs[granter].String()
	}
	if len(out.signers) > 0 {
		out.payer = out.signers[0]
		if feePayer >= 0 {
			out.payer = feePayer
		}
		if granter >= 0 && granter != out.payer {
			out.payer = granter
		}
	}

	edit, target, mode := h.Byte()%anteSigOps, h.Intn(max(len(out.signers), 1)), anteSignModes[h.Intn(len(anteSignModes))]
	out.forged = len(out.signers) > 0 && edit != anteSigNone && edit != anteSigOmitPubKey && edit != anteSigMode

	anys := make([]*codectypes.Any, len(msgs))
	for i, msg := range msgs {
		var err error
		anys[i], err = codectypes.NewAnyWithValue(msg)
		require.NoError(t, err)
	}
	body := txtypes.TxBody{Messages: anys, Memo: strings.Repeat("m", memoLen), TimeoutHeight: timeout}

	type sigSpec struct {
		acc             int
		mode, signAs    signing.SignMode
		seq, accNum     uint64
		chainID         string
		pubKey          cryptotypes.PubKey
		flip, emptySig  bool
		omitSig, dupSig bool
	}
	specs := make([]sigSpec, 0, len(out.signers)+1)
	newSpec := func(acc int) sigSpec {
		direct := signing.SignMode_SIGN_MODE_DIRECT
		return sigSpec{acc: acc, mode: direct, signAs: direct, accNum: fx.accNums[acc], chainID: testutil.ChainID, pubKey: fx.pubKeys[acc]}
	}
	for _, acc := range out.signers {
		specs = append(specs, newSpec(acc))
	}
	if len(specs) > 0 {
		s := &specs[target]
		switch edit {
		case anteSigSeqPlus:
			s.seq++
		case anteSigSeqMinus:
			s.seq--
		case anteSigAccNum:
			s.accNum++
		case anteSigChainID:
			s.chainID = "wrong-chain"
		case anteSigFlipByte:
			s.flip = true
		case anteSigEmpty:
			s.emptySig = true
		case anteSigOtherPubKey:
			s.pubKey = fx.pubKeys[(s.acc+1)%n]
		case anteSigOmitPubKey:
			s.pubKey, out.omitsPubKey = nil, true
		case anteSigDropSig:
			s.omitSig = true
		case anteSigExtraSig:
			s.dupSig = true
		case anteSigDropSigner:
			specs = append(specs[:target], specs[target+1:]...)
		case anteSigExtraSigner:
			other := (out.signers[len(out.signers)-1] + 1) % n
			specs = append(specs, newSpec(other))
		case anteSigMode:
			s.mode, s.signAs = mode, mode
		case anteSigModeMismatch:
			s.mode = mode
		}
	}
	for _, s := range specs {
		info := &txtypes.SignerInfo{Sequence: s.seq, ModeInfo: &txtypes.ModeInfo{Sum: &txtypes.ModeInfo_Single_{Single: &txtypes.ModeInfo_Single{Mode: s.mode}}}}
		if s.pubKey != nil {
			var err error
			info.PublicKey, err = codectypes.NewAnyWithValue(s.pubKey)
			require.NoError(t, err)
		}
		auth.SignerInfos = append(auth.SignerInfos, info)
	}

	var raw txtypes.TxRaw
	var err error
	raw.BodyBytes, err = body.Marshal()
	require.NoError(t, err)
	raw.AuthInfoBytes, err = auth.Marshal()
	require.NoError(t, err)
	unsigned, err := raw.Marshal()
	require.NoError(t, err)
	decoded, decodeErr := fx.txConfig.TxDecoder()(unsigned)
	for _, s := range specs {
		var sig []byte
		if decodeErr == nil && !s.emptySig {
			sig = fx.sign(ctx, decoded, s.acc, s.signAs, authsigning.SignerData{
				Address: fx.addrs[s.acc].String(), ChainID: s.chainID, AccountNumber: s.accNum, Sequence: s.seq, PubKey: fx.pubKeys[s.acc],
			})
		}
		if s.flip && len(sig) > 0 {
			sig[len(sig)/2] ^= 0x01
		}
		if !s.omitSig {
			raw.Signatures = append(raw.Signatures, sig)
		}
		if s.dupSig {
			raw.Signatures = append(raw.Signatures, sig)
		}
	}
	out.raw, err = raw.Marshal()
	require.NoError(t, err)
	return out
}

func appendUnique(s []int, v int) []int {
	if slices.Contains(s, v) {
		return s
	}
	return append(s, v)
}

var errAnteUnsignable = errors.New("tx cannot be signed in this mode")

// sign returns account key's signature in mode, or nil if the mode cannot
// sign the tx.
func (fx *anteChainFixture) sign(ctx sdk.Context, tx sdk.Tx, key int, mode signing.SignMode, data authsigning.SignerData) []byte {
	signBytes, err := func() (b []byte, err error) {
		defer func() {
			if recover() != nil {
				err = errAnteUnsignable
			}
		}()
		return authsigning.GetSignBytesAdapter(ctx, fx.txConfig.SignModeHandler(), mode, data, tx)
	}()
	if err != nil {
		return nil
	}
	sig, _, err := fx.kr.Sign(fx.names[key], signBytes, mode)
	if err != nil {
		return nil
	}
	return sig
}

// anteSnapshot is the check state the ante handler may change.
type anteSnapshot struct {
	balances  []sdk.Coins // per fixture account, then the fee collector
	sequences []uint64
}

func (fx *anteChainFixture) snapshot() anteSnapshot {
	ctx := fx.app.NewContext(true)
	var s anteSnapshot
	for _, addr := range fx.addrs {
		s.balances = append(s.balances, fx.app.BankKeeper.GetAllBalances(ctx, addr))
		s.sequences = append(s.sequences, fx.app.AccountKeeper.GetAccount(ctx, addr).GetSequence())
	}
	s.balances = append(s.balances, fx.app.BankKeeper.GetAllBalances(ctx, authtypes.NewModuleAddress(authtypes.FeeCollectorName)))
	return s
}

// check runs the tx built from script through CheckTx on fresh check state
// and checks the outcome. It reports whether the tx was accepted.
func (fx *anteChainFixture) check(t *testing.T, script []byte) bool {
	t.Helper()
	commitBlockAt(t, fx.app, recheckBlockTime)
	tx := fx.build(t, script)
	before := fx.snapshot()
	resp, err := fx.app.CheckTx(&abci.RequestCheckTx{Tx: tx.raw, Type: abci.CheckTxType_New})
	require.NotNil(t, resp)
	if err != nil {
		require.NotEqual(t, abci.CodeTypeOK, resp.Code)
	}
	recovered := resp.Codespace == sdkerrors.RootCodespace && resp.Code == sdkerrors.ErrPanic.ABCICode() ||
		strings.Contains(resp.Log, "panic")
	// The SDK's signing adapter panics on a signer without a public key by
	// design (celestia-app#4847); baseapp turns it into a rejection.
	nilPubKey := tx.omitsPubKey && strings.Contains(resp.Log, "signerInfo.PublicKey cannot be nil")
	// ValidateTxFee panics on fees above MaxInt64, pending PROTOCO-3433 item 2.
	hugeFee := !tx.fee.AmountOf(appconsts.BondDenom).IsInt64() && strings.Contains(resp.Log, "Int64() out of bound")
	if recovered && !nilPubKey && !hugeFee && !knownAntePanic(fx.app.GetTxConfig().TxDecoder(), [][]byte{tx.raw}) {
		t.Fatalf("CheckTx recovered a panic:\n%s", resp.Log)
	}
	after := fx.snapshot()
	if resp.Code != abci.CodeTypeOK {
		require.Equal(t, before, after, "rejected tx changed the check state: %s", resp.Log)
		return false
	}

	require.False(t, tx.forged, "accepted a tx with a forged signature")
	require.GreaterOrEqual(t, tx.payer, 0)
	require.Equal(t, int64(tx.gas), resp.GasWanted) //nolint:gosec // accepted gas limits fit the SDK gas budget
	require.LessOrEqual(t, resp.GasUsed, resp.GasWanted)
	require.GreaterOrEqual(t, uint64(resp.GasUsed), uint64(len(tx.raw))*appconsts.TxSizeCostPerByte) //nolint:gosec // non-negative
	collector := len(fx.addrs)
	require.Equal(t, before.balances[collector].Add(tx.fee...), after.balances[collector], "fee collector")
	for i := range fx.addrs {
		wantBalance, wantSeq := before.balances[i], before.sequences[i]
		if i == tx.payer {
			var neg bool
			wantBalance, neg = wantBalance.SafeSub(tx.fee...)
			require.False(t, neg)
		}
		for _, s := range tx.signers {
			if s == i {
				wantSeq++
			}
		}
		require.Equal(t, wantBalance, after.balances[i], "balance of account %d", i)
		require.Equal(t, wantSeq, after.sequences[i], "sequence of account %d", i)
	}
	return true
}

func withAnteChainFixture(t *testing.T, fn func(fx *anteChainFixture)) {
	t.Helper()
	recheckFixtureMu.Lock() // the fixtures share recheckBlockTime and commit blocks
	defer recheckFixtureMu.Unlock()
	if anteChainFx == nil || anteChainFx.app.LastBlockHeight()-anteChainFx.start > anteFixtureBlocks {
		if anteChainFx != nil {
			require.NoError(t, anteChainFx.app.Close())
		}
		anteChainFx = newAnteChainFixture(t)
	}
	fn(anteChainFx)
}

// anteSeed lays out a header and message records. Header bytes are: 0
// message count, 1 fee mode, 2 denom mode, 3-10 fee amount, 11 gas mode, 12-15
// gas, 16 memo, 17 timeout, 18 fee payer, 19 granter, 20 signature edit, 21
// target, 22 sign mode.
func anteSeed(header []byte, msgs ...[]byte) []byte {
	return append(fuzzutil.Records(anteHeaderRecord, header), fuzzutil.Records(anteMsgRecord, msgs...)...)
}

func anteHeader(count, feePayer, granter, edit, mode byte) []byte {
	h := make([]byte, 24)
	h[0], h[18], h[19], h[20], h[22] = count, feePayer, granter, edit, mode
	return h
}

func anteSeeds() [][]byte {
	pff := anteSeed(anteHeader(0, 0, 0, 0, 0), []byte{1})
	seeds := make([][]byte, 0, 32)
	seeds = append(seeds,
		anteSeed(nil),
		pff,
		anteSeed(anteHeader(2, 0, 0, 0, 0), []byte{0, 0}, []byte{0, 1}, []byte{0, 2}),
		anteSeed(anteHeader(0, 0, 3, 0, 0), []byte{0, 0}),                 // granted fee
		anteSeed(anteHeader(0, 2, 0, 0, 0), []byte{0, 0}),                 // explicit fee payer
		anteSeed(anteHeader(0, 0, 0, anteSigMode, 0), []byte{0, 0}),       // amino JSON
		anteSeed(anteHeader(0, 0, 0, anteSigOmitPubKey, 0), []byte{0, 1}), // pubkey on chain
	)
	for edit := range byte(anteSigOps) {
		seeds = append(seeds, anteSeed(anteHeader(1, 0, 0, edit, 1), []byte{0, 0}, []byte{0, 2}))
	}
	for i := range 6 {
		h := anteHeader(0, 0, 0, 0, 0)
		h[1], h[2], h[11], h[16], h[17] = byte(i), byte(i%3), byte(i%5), byte(i%4), byte(i%5)
		seeds = append(seeds, anteSeed(h, []byte{byte(i % 4), 0, 0, 0, 0, 0}))
	}
	return seeds
}

// TestAnteChainSeeds keeps the oracle meaningful: the unmodified send,
// multi-signer, granted, fee-payer and pay-for-fibre txs must be accepted.
func TestAnteChainSeeds(t *testing.T) {
	withAnteChainFixture(t, func(fx *anteChainFixture) {
		for i, seed := range anteSeeds() {
			t.Run(fmt.Sprint(i), func(t *testing.T) {
				accepted := fx.check(t, seed)
				if i < 6 {
					require.True(t, accepted, "seed %d must be accepted", i)
				}
			})
		}
	})
}

// FuzzAnteChainSignedTxs builds txs from typed fields (messages and signers,
// fee amount and denoms, gas limit, memo length, timeout height, fee payer and
// granter, sign modes, public keys and signature counts) and signs them
// correctly unless the input forges a signature, so edits reach every ante
// decorator past signature verification, including the fibre decorators. A
// forged tx must be rejected; an accepted tx must deduct its fee once from the
// right account, advance each signer's sequence by one and charge at least the
// tx size gas; a rejected tx must leave the check state unchanged.
func FuzzAnteChainSignedTxs(f *testing.F) {
	for _, seed := range anteSeeds() {
		f.Add(seed)
	}
	f.Fuzz(func(t *testing.T, script []byte) {
		if len(script) > anteHeaderRecord+anteMaxMsgs*anteMsgRecord {
			return
		}
		withAnteChainFixture(t, func(fx *anteChainFixture) { fx.check(t, script) })
	})
}

package app

import (
	"errors"
	"math"
	"strconv"
	"testing"

	"cosmossdk.io/log"
	"cosmossdk.io/store"
	"cosmossdk.io/store/metrics"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/test/util/blobfactory"
	fibrekeeper "github.com/celestiaorg/celestia-app/v11/x/fibre/keeper"
	fibretypes "github.com/celestiaorg/celestia-app/v11/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	dbm "github.com/cosmos/cosmos-db"
	"github.com/cosmos/cosmos-sdk/baseapp"
	"github.com/cosmos/cosmos-sdk/client"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/stretchr/testify/require"
)

// pffFill runs Fill with decodePFF counting its calls and preverifyPFF recording its input
// when preverify is set. The ante handler rejects the txs in reject; router settles the messages.
type pffFill struct {
	txConfig     client.TxConfig
	preverify    bool
	reject       map[string]bool
	router       baseapp.MessageRouter
	anteCalls    int
	sdkDecodes   int
	pffDecodes   int
	preverified  [][]byte
	preverifyRan bool
}

func (p *pffFill) run(t *testing.T, txs [][]byte) [][]byte {
	t.Helper()
	handler := func(ctx sdk.Context, _ sdk.Tx, _ bool) (sdk.Context, error) {
		p.anteCalls++
		if p.reject[string(ctx.TxBytes())] {
			return ctx, errors.New("rejected")
		}
		return ctx, nil
	}
	fsb, err := NewFilteredSquareBuilder(handler, p.router, countingTxConfig{p.txConfig, &p.sdkDecodes}, fakeChannelKeeper{}, appconsts.SquareSizeUpperBound, appconsts.SubtreeRootThreshold)
	require.NoError(t, err)
	fsb.decodePFF = func(raw []byte) *fibretypes.DecodedPayForFibre {
		p.pffDecodes++
		return fibrekeeper.DecodePayForFibre(raw)
	}
	if p.preverify {
		fsb.preverifyPFF = func(_ sdk.Context, decoded []*fibretypes.DecodedPayForFibre) {
			p.preverifyRan = true
			for _, d := range decoded {
				p.preverified = append(p.preverified, d.Raw)
			}
		}
	}
	ms := store.NewCommitMultiStore(dbm.NewMemDB(), log.NewNopLogger(), metrics.NewNoOpMetrics())
	ctx := sdk.NewContext(ms, cmtproto.Header{}, false, log.NewNopLogger())
	return fsb.Fill(ctx, txs, math.MaxInt64)
}

func newPFFs(t *testing.T, txConfig client.TxConfig, n int) [][]byte {
	t.Helper()
	txs := make([][]byte, n)
	for i := range txs {
		txs[i] = blobfactory.UnsignedPayForFibreTx(t, txConfig)
	}
	return txs
}

// TestFillStopsPFFDecodesAtCap pins that no PayForFibre tx is decoded past the one that fills the
// cap, beyond the pre-verified ones, and that no tx is decoded twice.
func TestFillStopsPFFDecodesAtCap(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	txs := newPFFs(t, txConfig, 2*appconsts.MaxPayForFibreCandidates)

	_, _, _, entries := separateTxsDecoded(log.NewNopLogger(), countingTxConfig{txConfig, new(int)}, txs)
	require.Len(t, entries, len(txs))
	for i, e := range entries {
		require.Equal(t, txs[i], e.raw)
		require.Nil(t, e.sdkTx, "separateTxsDecoded decoded PFF %d", i)
	}

	lazy := &pffFill{txConfig: txConfig}
	require.Equal(t, txs[:appconsts.MaxPayForFibreMessages], lazy.run(t, txs))
	require.Equal(t, appconsts.MaxPayForFibreMessages, lazy.sdkDecodes)
	require.Equal(t, appconsts.MaxPayForFibreMessages, lazy.pffDecodes)

	warm := &pffFill{txConfig: txConfig, preverify: true}
	require.Equal(t, txs[:appconsts.MaxPayForFibreMessages], warm.run(t, txs))
	require.Equal(t, appconsts.MaxPayForFibreCandidates, warm.sdkDecodes)
	require.Equal(t, appconsts.MaxPayForFibreCandidates, warm.pffDecodes)
}

// TestFillKeepsValidPFFsBehindRejectedOnes pins the fix: PayForFibre txs that fail ante past the
// pre-verification bound no longer evict the valid ones queued behind them.
func TestFillKeepsValidPFFsBehindRejectedOnes(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	rejected := newPFFs(t, txConfig, appconsts.MaxPayForFibreCandidates)
	valid := newPFFs(t, txConfig, 10)
	reject := make(map[string]bool, len(rejected))
	for _, tx := range rejected {
		reject[string(tx)] = true
	}

	fill := &pffFill{txConfig: txConfig, preverify: true, reject: reject}
	require.Equal(t, valid, fill.run(t, append(append([][]byte{}, rejected...), valid...)))
	require.Len(t, fill.preverified, appconsts.MaxPayForFibreCandidates)
}

// TestFillPreverifiesTheCandidateSet pins that the warm-up gets what the old candidate bound kept:
// the first MaxPayForFibreCandidates PayForFibre txs that pass the shape check, in order.
func TestFillPreverifiesTheCandidateSet(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	invalid := [][]byte{
		newMultiPayForFibreTx(t, txConfig),
		newMixedPayForFibreTx(t, txConfig),
		newPayForFibreTxWithNamespace(t, txConfig, share.TxNamespace.Bytes()),
		[]byte("garbage"),
	}
	var txs, want [][]byte
	for i := range appconsts.MaxPayForFibreCandidates + 50 {
		if i%5 == 0 {
			txs = append(txs, invalid[(i/5)%len(invalid)])
		}
		tx := blobfactory.UnsignedPayForFibreTx(t, txConfig)
		txs = append(txs, tx)
		if len(want) < appconsts.MaxPayForFibreCandidates {
			want = append(want, tx)
		}
	}

	fill := &pffFill{txConfig: txConfig, preverify: true}
	fill.run(t, txs)
	require.True(t, fill.preverifyRan)
	require.Equal(t, want, fill.preverified)
}

// settleOnceRouter settles each payment promise once, like the processed-payment record.
type settleOnceRouter map[string]bool

func (r settleOnceRouter) Handler(sdk.Msg) baseapp.MsgServiceHandler {
	return func(_ sdk.Context, msg sdk.Msg) (*sdk.Result, error) {
		key := string(msg.(*fibretypes.MsgPayForFibre).PaymentPromise.SignerPublicKey.Key)
		if r[key] {
			return nil, errors.New("promise already processed")
		}
		r[key] = true
		return &sdk.Result{}, nil
	}
}

func (settleOnceRouter) HandlerByTypeURL(string) baseapp.MsgServiceHandler { return nil }

// TestFillSkipsSettledPromiseCopies pins that copies of a promise already settled in the proposal
// skip the ante: a flood of one promise in many txs costs one ante, not one per copy.
func TestFillSkipsSettledPromiseCopies(t *testing.T) {
	txConfig := encoding.MakeConfig(ModuleEncodingRegisters...).TxConfig
	msg := blobfactory.NewMsgPayForFibre(t, secp256k1.GenPrivKey().PubKey().(*secp256k1.PubKey), "test")
	copies := make([][]byte, 2*appconsts.MaxPayForFibreCandidates)
	for i := range copies {
		builder := txConfig.NewTxBuilder()
		require.NoError(t, builder.SetMsgs(msg))
		builder.SetMemo(strconv.Itoa(i))
		raw, err := txConfig.TxEncoder()(builder.GetTx())
		require.NoError(t, err)
		copies[i] = raw
	}
	other := blobfactory.UnsignedPayForFibreTx(t, txConfig)

	fill := &pffFill{txConfig: txConfig, preverify: true, router: settleOnceRouter{}}
	require.Equal(t, [][]byte{copies[0], other}, fill.run(t, append(copies, other)))
	require.Equal(t, 2, fill.anteCalls)
}

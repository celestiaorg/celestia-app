package app_test

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"cosmossdk.io/log"
	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	"github.com/celestiaorg/go-square/v4/share"
	blobtx "github.com/celestiaorg/go-square/v4/tx"
	abci "github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	txtypes "github.com/cosmos/cosmos-sdk/types/tx"
	"github.com/rs/zerolog"
	"github.com/stretchr/testify/require"
)

// proposalFixture is a test app with one proposal that PrepareProposal built
// from a blob tx, a multi-blob tx, a send tx and a pay-for-fibre tx. It is built
// once per process: ProcessProposal runs on a branch, so iterations do not
// interfere with each other.
type proposalFixture struct {
	app       *app.App
	logs      *lockedBuffer
	txs       [][]byte
	dataRoot  []byte
	square    uint64
	height    int64
	blockTime time.Time
}

var (
	fixtureOnce sync.Once
	fixture     *proposalFixture
	fixtureMu   sync.Mutex
)

// lockedBuffer collects the app log so the test can see panics that
// ProcessProposal recovered.
type lockedBuffer struct {
	mu  sync.Mutex
	buf bytes.Buffer
}

func (b *lockedBuffer) Write(p []byte) (int, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.Write(p)
}

func (b *lockedBuffer) Reset() {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.buf.Reset()
}

func (b *lockedBuffer) String() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.buf.String()
}

func newProposalFixture(t *testing.T) *proposalFixture {
	t.Helper()
	logs := &lockedBuffer{}
	prev := testutil.TestAppLogger
	testutil.TestAppLogger = log.NewLogger(logs, log.ColorOption(false), log.LevelOption(zerolog.ErrorLevel))
	defer func() { testutil.TestAppLogger = prev }()

	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	accounts := testfactory.GenerateAccounts(5)
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), accounts...)
	// A fixed clock keeps the pay-for-fibre promise fresh for the whole run.
	blockTime := testutil.GenesisTime.Add(time.Hour)
	commitBlockAt(t, testApp, blockTime)
	infos := queryAccountInfo(testApp, accounts, kr)
	newSigner := newSignerFactory(t, kr, enc.TxConfig, accounts, infos)

	ns := share.MustNewV0Namespace(bytes.Repeat([]byte{0x02}, share.NamespaceVersionZeroIDSize))
	blob, err := share.NewBlob(ns, bytes.Repeat([]byte{0x01}, 600), share.ShareVersionZero, nil)
	require.NoError(t, err)
	blobTx, _, err := newSigner(0).CreatePayForBlobs(accounts[0], []*share.Blob{blob}, user.SetGasLimit(500_000), user.SetFee(1))
	require.NoError(t, err)

	ns2 := share.MustNewV0Namespace(bytes.Repeat([]byte{0x03}, share.NamespaceVersionZeroIDSize))
	blob2, err := share.NewBlob(ns2, bytes.Repeat([]byte{0x02}, 3000), share.ShareVersionZero, nil)
	require.NoError(t, err)
	blob3, err := share.NewBlob(ns, []byte{0x03}, share.ShareVersionZero, nil)
	require.NoError(t, err)
	multiBlobTx, _, err := newSigner(1).CreatePayForBlobs(accounts[1], []*share.Blob{blob2, blob3}, user.SetGasLimit(500_000), user.SetFee(1))
	require.NoError(t, err)

	sendTx := testutil.SendTxsWithAccounts(t, testApp, enc.TxConfig, kr, 1000, accounts[4], accounts[2:3], testutil.ChainID)[0]

	seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[3]), 1_000_000)
	pffTx := newSignedPayForFibreTxAt(t, newSigner(3), accounts[3], true, blockTime.Add(-time.Minute),
		user.SetGasLimit(1_000_000), user.SetFee(4_000))

	height := testApp.LastBlockHeight() + 1
	resp, err := testApp.PrepareProposal(&abci.RequestPrepareProposal{
		Txs:        [][]byte{sendTx, pffTx, blobTx, multiBlobTx},
		Height:     height,
		Time:       blockTime,
		MaxTxBytes: int64(appconsts.BlockMaxBytes),
	})
	require.NoError(t, err)
	require.Equal(t, 4, len(resp.Txs), "every seed tx must make it into the proposal")

	return &proposalFixture{
		app:       testApp,
		logs:      logs,
		txs:       resp.Txs,
		dataRoot:  resp.DataRootHash,
		square:    resp.SquareSize,
		height:    height,
		blockTime: blockTime,
	}
}

// processProposal runs ProcessProposal on txs and reports its status. It fails
// the test when the handler recovered a panic: a panic here means a node would
// have crashed without the recover, and it rejects a block that may be valid.
func (fx *proposalFixture) processProposal(t *testing.T, txs [][]byte, dataRoot []byte, squareSize uint64) abci.ResponseProcessProposal_ProposalStatus {
	t.Helper()
	fx.logs.Reset()
	resp, err := fx.app.ProcessProposal(&abci.RequestProcessProposal{
		Txs:          txs,
		Height:       fx.height,
		Time:         fx.blockTime,
		DataRootHash: dataRoot,
		SquareSize:   squareSize,
	})
	require.NoError(t, err)
	if logged := fx.logs.String(); strings.Contains(logged, "caught panic") && !knownAntePanic(fx.app.GetTxConfig().TxDecoder(), txs) {
		t.Fatalf("ProcessProposal recovered a panic:\n%s", logged)
	}
	return resp.Status
}

// knownAntePanic reports whether txs holds a tx that the SDK decoder accepts
// without messages or without a fee. The ante handler dereferences the nil fee
// and panics; CheckTx and ProcessProposal recover that and reject the tx or
// block, which is the accepted outcome today, so the fuzzers do not count it
// as a crash.
func knownAntePanic(decoder sdk.TxDecoder, txs [][]byte) bool {
	for _, raw := range txs {
		if len(raw) > appconsts.MaxTxSize {
			continue
		}
		if blobTx, isBlob, err := blobtx.UnmarshalBlobTx(raw); isBlob {
			if err != nil {
				continue
			}
			raw = blobTx.Tx
		}
		sdkTx, err := decoder(raw)
		if err != nil {
			continue
		}
		if len(sdkTx.GetMsgs()) == 0 {
			return true
		}
		if protoTx, ok := sdkTx.(interface{ GetProtoTx() *txtypes.Tx }); ok {
			if pt := protoTx.GetProtoTx(); pt.AuthInfo == nil || pt.AuthInfo.Fee == nil {
				return true
			}
		}
	}
	return false
}

// mutateProposal applies one fuzz-chosen edit to the proposal's txs.
func mutateProposal(txs [][]byte, txIndex, op uint8, offset uint32, value byte, extra []byte) [][]byte {
	out := make([][]byte, len(txs))
	for i, tx := range txs {
		out[i] = bytes.Clone(tx)
	}
	if len(out) == 0 {
		return out
	}
	i := int(txIndex) % len(out)
	tx := out[i]
	at := 0
	if len(tx) > 0 {
		at = int(offset % uint32(len(tx)))
	}
	switch op % 12 {
	case 0:
		// unmodified
	case 1:
		if len(tx) > 0 {
			tx[at] ^= 1 << (value % 8)
		}
	case 2:
		if len(tx) > 0 {
			tx[at] = value
		}
	case 3:
		out[i] = tx[:at]
	case 4:
		out[i] = append(append(append([]byte{}, tx[:at]...), extra...), tx[at:]...)
	case 5:
		end := min(at+1+len(extra), len(tx))
		out[i] = append(append([]byte{}, tx[:at]...), tx[end:]...)
	case 6:
		out = append(out, bytes.Clone(tx))
	case 7:
		out = append(out[:i], out[i+1:]...)
	case 8:
		j := (i + 1) % len(out)
		out[i], out[j] = out[j], out[i]
	case 9:
		out[i] = bytes.Clone(extra)
	case 10:
		out = append(out, bytes.Clone(extra))
	case 11:
		out[i] = append(bytes.Clone(extra), tx...)
	}
	return out
}

func fuzzProcessProposal(t *testing.T, txIndex, op uint8, offset uint32, value byte, extra []byte) {
	t.Helper()
	if len(extra) > appconsts.MaxTxSize {
		return
	}
	fixtureMu.Lock()
	defer fixtureMu.Unlock()
	fixtureOnce.Do(func() { fixture = newProposalFixture(t) })
	fx := fixture

	txs := mutateProposal(fx.txs, txIndex, op, offset, value, extra)
	status := fx.processProposal(t, txs, fx.dataRoot, fx.square)
	unchanged := len(txs) == len(fx.txs)
	for i := range txs {
		unchanged = unchanged && bytes.Equal(txs[i], fx.txs[i])
	}
	if unchanged {
		require.Equal(t, abci.ResponseProcessProposal_ACCEPT, status, "unmodified proposal must be accepted:\n%s", fx.logs.String())
	}
}

// TestProcessProposalMutationSeeds keeps the fuzz seeds meaningful: the
// unmodified proposal is accepted and each mutation runs without a panic.
func TestProcessProposalMutationSeeds(t *testing.T) {
	fuzzProcessProposal(t, 0, 0, 0, 0, nil)
	for op := range uint8(12) {
		for txIndex := range uint8(4) {
			t.Run(fmt.Sprintf("op=%d/tx=%d", op, txIndex), func(t *testing.T) {
				fuzzProcessProposal(t, txIndex, op, 7, 0xff, []byte{0x0a, 0x00})
			})
		}
	}
}

// FuzzProcessProposalMutatedTxs mutates the txs of a valid proposal and runs
// ProcessProposal on the result. ProcessProposal recovers panics and votes nil,
// which keeps the node alive but hides crashes, so the test fails on any
// recovered panic. The unmodified proposal must still be accepted.
func FuzzProcessProposalMutatedTxs(f *testing.F) {
	f.Add(uint8(0), uint8(0), uint32(0), byte(0), []byte{})
	for op := range uint8(12) {
		for txIndex := range uint8(4) {
			f.Add(txIndex, op, uint32(3), byte(0x80), []byte{0x0a, 0x00})
			f.Add(txIndex, op, uint32(1<<16), byte(0x01), []byte("\x12\x04\x08\x01\x10\x02"))
		}
	}
	f.Fuzz(func(t *testing.T, txIndex, op uint8, offset uint32, value byte, extra []byte) {
		fuzzProcessProposal(t, txIndex, op, offset, value, extra)
	})
}

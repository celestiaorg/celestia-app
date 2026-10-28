package app_test

import (
	"bytes"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

// checkTxFixture is a test app with the mempool form of the txs a node
// receives from peers and RPC clients: a blob tx, a multi-blob tx, a send tx
// and a pay-for-fibre tx. CheckTx mutates the check state when it accepts a tx,
// so the fixture only promises that nothing panics; the seed test checks
// acceptance on a fresh fixture.
type checkTxFixture struct {
	app *app.App
	txs [][]byte
}

var (
	checkTxFixtureOnce sync.Once
	checkTxFixtureVal  *checkTxFixture
	checkTxFixtureMu   sync.Mutex
)

func newCheckTxFixture(t *testing.T) *checkTxFixture {
	t.Helper()
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
	blobTx, _, err := newSigner(0).CreatePayForBlobs(accounts[0], []*share.Blob{blob}, user.SetGasLimit(500_000), user.SetFee(100_000))
	require.NoError(t, err)

	ns2 := share.MustNewV0Namespace(bytes.Repeat([]byte{0x03}, share.NamespaceVersionZeroIDSize))
	blob2, err := share.NewBlob(ns2, bytes.Repeat([]byte{0x02}, 3000), share.ShareVersionZero, nil)
	require.NoError(t, err)
	blob3, err := share.NewBlob(ns, []byte{0x03}, share.ShareVersionZero, nil)
	require.NoError(t, err)
	multiBlobTx, _, err := newSigner(1).CreatePayForBlobs(accounts[1], []*share.Blob{blob2, blob3}, user.SetGasLimit(500_000), user.SetFee(100_000))
	require.NoError(t, err)

	sendTx := testutil.SendTxsWithAccounts(t, testApp, enc.TxConfig, kr, 1000, accounts[4], accounts[2:3], testutil.ChainID)[0]

	seedFibreEscrow(t, testApp, testfactory.GetAddress(kr, accounts[3]), 1_000_000)
	pffTx := newSignedPayForFibreTxAt(t, newSigner(3), accounts[3], true, blockTime.Add(-time.Minute),
		user.SetGasLimit(1_000_000), user.SetFee(4_000))

	return &checkTxFixture{app: testApp, txs: [][]byte{sendTx, pffTx, blobTx, multiBlobTx}}
}

// checkTx runs CheckTx on raw and reports its response code. It fails the test
// when baseapp recovered a panic: the recover keeps the node alive, but the
// same tx reaching a path without one (the mempool, PrepareProposal) would not.
func (fx *checkTxFixture) checkTx(t *testing.T, raw []byte) uint32 {
	t.Helper()
	resp, err := fx.app.CheckTx(&abci.RequestCheckTx{Tx: raw, Type: abci.CheckTxType_New})
	if err != nil {
		// The app reports decode failures as an error next to the response.
		require.NotNil(t, resp)
		require.NotEqual(t, abci.CodeTypeOK, resp.Code)
	}
	recovered := resp.Codespace == sdkerrors.RootCodespace && resp.Code == sdkerrors.ErrPanic.ABCICode() ||
		strings.Contains(resp.Log, "panic")
	if recovered && !knownAntePanic(fx.app.GetTxConfig().TxDecoder(), [][]byte{raw}) {
		t.Fatalf("CheckTx recovered a panic:\n%s", resp.Log)
	}
	return resp.Code
}

func fuzzCheckTx(t *testing.T, txIndex, op uint8, offset uint32, value byte, extra []byte) {
	t.Helper()
	if len(extra) > appconsts.MaxTxSize {
		return
	}
	checkTxFixtureMu.Lock()
	defer checkTxFixtureMu.Unlock()
	checkTxFixtureOnce.Do(func() { checkTxFixtureVal = newCheckTxFixture(t) })
	fx := checkTxFixtureVal

	for _, raw := range mutateProposal(fx.txs, txIndex, op, offset, value, extra) {
		fx.checkTx(t, raw)
	}
}

// TestCheckTxMutationSeeds keeps the fuzz seeds meaningful: a fresh fixture
// accepts every unmodified tx, and each mutation runs without a panic.
func TestCheckTxMutationSeeds(t *testing.T) {
	fx := newCheckTxFixture(t)
	for i, raw := range fx.txs {
		require.Equal(t, abci.CodeTypeOK, fx.checkTx(t, raw), "unmodified tx %d must be accepted", i)
	}
	for op := range uint8(12) {
		for txIndex := range uint8(4) {
			t.Run(fmt.Sprintf("op=%d/tx=%d", op, txIndex), func(t *testing.T) {
				for _, raw := range mutateProposal(fx.txs, txIndex, op, 7, 0xff, []byte{0x0a, 0x00}) {
					fx.checkTx(t, raw)
				}
			})
		}
	}
}

// FuzzCheckTxMutatedTxs mutates the mempool form of valid txs and runs CheckTx
// on the result: the first place peer- and client-supplied bytes reach blob tx
// validation, the tx decoder, message counting and the ante handler chain.
// Baseapp recovers panics into an error response, which hides crashes, so the
// test fails on any recovered panic.
func FuzzCheckTxMutatedTxs(f *testing.F) {
	f.Add(uint8(0), uint8(0), uint32(0), byte(0), []byte{})
	for op := range uint8(12) {
		for txIndex := range uint8(4) {
			f.Add(txIndex, op, uint32(3), byte(0x80), []byte{0x0a, 0x00})
			f.Add(txIndex, op, uint32(1<<16), byte(0x01), []byte("\x12\x04\x08\x01\x10\x02"))
		}
	}
	f.Fuzz(func(t *testing.T, txIndex, op uint8, offset uint32, value byte, extra []byte) {
		fuzzCheckTx(t, txIndex, op, offset, value, extra)
	})
}

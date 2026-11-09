package app_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	abci "github.com/cometbft/cometbft/abci/types"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

var (
	recheckFixtureMu    sync.Mutex
	recheckFixture      *checkTxFixture
	recheckFixtureStart int64
)

// recheckFixtureBlocks bounds how many blocks one fixture serves. Every input
// commits two blocks, and the pay-for-fibre promise is only valid for a window
// of blocks after its height, so the fixture is rebuilt well before then.
const recheckFixtureBlocks = 200

// recheckBlockTime matches the block time newCheckTxFixture commits at, so the
// pay-for-fibre promise stays fresh across the empty blocks committed here.
var recheckBlockTime = testutil.GenesisTime.Add(time.Hour)

// runCheckTx runs CheckTx of the given type and fails on a recovered panic.
func (fx *checkTxFixture) runCheckTx(t *testing.T, raw []byte, typ abci.CheckTxType) *abci.ResponseCheckTx {
	t.Helper()
	resp, err := fx.app.CheckTx(&abci.RequestCheckTx{Tx: raw, Type: typ})
	if err != nil {
		require.NotNil(t, resp)
		require.NotEqual(t, abci.CodeTypeOK, resp.Code)
	}
	recovered := resp.Codespace == sdkerrors.RootCodespace && resp.Code == sdkerrors.ErrPanic.ABCICode() ||
		strings.Contains(resp.Log, "panic")
	if recovered && !knownAntePanic(fx.app.GetTxConfig().TxDecoder(), [][]byte{raw}) {
		t.Fatalf("CheckTx (%s) recovered a panic:\n%s", typ, resp.Log)
	}
	return resp
}

// fuzzRecheck admits mutated txs with a new CheckTx, commits an empty block,
// which resets the check state as after any block, and rechecks every admitted
// tx as the mempool does.
func fuzzRecheck(t *testing.T, txIndex, op uint8, offset uint32, value byte, extra []byte) {
	t.Helper()
	if len(extra) > appconsts.MaxTxSize {
		return
	}
	recheckFixtureMu.Lock()
	defer recheckFixtureMu.Unlock()
	if recheckFixture == nil || recheckFixture.app.LastBlockHeight()-recheckFixtureStart > recheckFixtureBlocks {
		if recheckFixture != nil {
			require.NoError(t, recheckFixture.app.Close())
		}
		recheckFixture = newCheckTxFixture(t)
		recheckFixtureStart = recheckFixture.app.LastBlockHeight()
	}
	fx := recheckFixture

	// Start from committed state so earlier inputs' admissions do not leak in.
	commitBlockAt(t, fx.app, recheckBlockTime)
	var admitted [][]byte
	for _, raw := range mutateProposal(fx.txs, txIndex, op, offset, value, extra) {
		// The mempool only ever rechecks txs a new CheckTx admitted.
		if fx.runCheckTx(t, raw, abci.CheckTxType_New).Code == abci.CodeTypeOK {
			admitted = append(admitted, raw)
			// A recheck before the next block runs on the same check state.
			fx.runCheckTx(t, raw, abci.CheckTxType_Recheck)
		}
	}

	commitBlockAt(t, fx.app, recheckBlockTime)
	for i, raw := range admitted {
		resp := fx.runCheckTx(t, raw, abci.CheckTxType_Recheck)
		// Nothing changed the state these txs depend on, so a recheck that
		// evicts one drops a valid tx from every mempool.
		require.Equal(t, abci.CodeTypeOK, resp.Code, "recheck evicted admitted tx %d: %s", i, resp.Log)
	}
}

// TestRecheckSeeds checks that every unmodified fixture tx survives a recheck
// and that each seed mutation runs.
func TestRecheckSeeds(t *testing.T) {
	for op := range uint8(12) {
		for txIndex := range uint8(4) {
			t.Run(fmt.Sprintf("op=%d/tx=%d", op, txIndex), func(t *testing.T) {
				fuzzRecheck(t, txIndex, op, 7, 0xff, []byte{0x0a, 0x00})
			})
		}
	}
}

// FuzzRecheckMutatedTxs mutates the mempool form of valid txs, admits what a
// new CheckTx accepts, and rechecks the admitted txs before and after an empty
// block, as the mempool does after every commit. Rechecks must not recover a
// panic, and after a block that changed nothing they must keep every admitted
// tx. The recheck path skips blob validation and, for pay-for-fibre txs, the
// app's CheckTx wrapper, relying on the new CheckTx having checked the same
// bytes.
func FuzzRecheckMutatedTxs(f *testing.F) {
	f.Add(uint8(0), uint8(0), uint32(0), byte(0), []byte{})
	for op := range uint8(12) {
		for txIndex := range uint8(4) {
			f.Add(txIndex, op, uint32(3), byte(0x80), []byte{0x0a, 0x00})
			f.Add(txIndex, op, uint32(1<<16), byte(0x01), []byte("\x12\x04\x08\x01\x10\x02"))
		}
	}
	f.Fuzz(func(t *testing.T, txIndex, op uint8, offset uint32, value byte, extra []byte) {
		fuzzRecheck(t, txIndex, op, offset, value, extra)
	})
}

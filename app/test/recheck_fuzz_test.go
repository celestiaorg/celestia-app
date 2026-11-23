package app_test

import (
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	abci "github.com/cometbft/cometbft/abci/types"
	simtestutil "github.com/cosmos/cosmos-sdk/testutil/sims"
	sdkerrors "github.com/cosmos/cosmos-sdk/types/errors"
	"github.com/stretchr/testify/require"
)

var (
	recheckFixtureMu    sync.Mutex
	recheckFixtures     [2]*checkTxFixture // a stock app and a fast recheck app
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

// newRecheckFixtures builds the same fixture on a stock app and on a fast
// recheck app. Both are deterministic, so they start from the same state.
func newRecheckFixtures(t *testing.T) [2]*checkTxFixture {
	stock := newCheckTxFixtureWithOptions(t, simtestutil.AppOptionsMap{app.FlagFastRecheck: false})
	fast := newCheckTxFixtureWithOptions(t, simtestutil.AppOptionsMap{app.FlagFastRecheck: true})
	require.Equal(t, stock.txs, fast.txs)
	require.Equal(t, stock.app.LastCommitID(), fast.app.LastCommitID())
	return [2]*checkTxFixture{stock, fast}
}

// fuzzRecheck admits mutated txs with a new CheckTx, commits an empty block,
// which resets the check state as after any block, and rechecks every admitted
// tx as the mempool does. Every step runs on a stock app and a fast recheck
// app, which must reach the same verdicts.
func fuzzRecheck(t *testing.T, txIndex, op uint8, offset uint32, value byte, extra []byte) {
	t.Helper()
	if len(extra) > appconsts.MaxTxSize {
		return
	}
	recheckFixtureMu.Lock()
	defer recheckFixtureMu.Unlock()
	if fx := recheckFixtures[0]; fx == nil || fx.app.LastBlockHeight()-recheckFixtureStart > recheckFixtureBlocks {
		for _, fx := range recheckFixtures {
			if fx != nil {
				require.NoError(t, fx.app.Close())
			}
		}
		recheckFixtures = newRecheckFixtures(t)
		recheckFixtureStart = recheckFixtures[0].app.LastBlockHeight()
	}
	stock, fast := recheckFixtures[0], recheckFixtures[1]
	commit := func() {
		commitBlockAt(t, stock.app, recheckBlockTime)
		commitBlockAt(t, fast.app, recheckBlockTime)
		require.Equal(t, stock.app.LastCommitID(), fast.app.LastCommitID())
	}
	// checkTx runs CheckTx on both apps and returns the stock response.
	checkTx := func(raw []byte, typ abci.CheckTxType) *abci.ResponseCheckTx {
		want, got := stock.runCheckTx(t, raw, typ), fast.runCheckTx(t, raw, typ)
		require.Equal(t, want.Code, got.Code, "%s code differs: stock %q, fast %q", typ, want.Log, got.Log)
		if typ == abci.CheckTxType_Recheck {
			require.Equal(t, want.GasWanted, got.GasWanted, "recheck gas wanted differs")
		}
		return want
	}

	// Start from committed state so earlier inputs' admissions do not leak in.
	commit()
	var admitted [][]byte
	for _, raw := range mutateProposal(stock.txs, txIndex, op, offset, value, extra) {
		// The mempool only ever rechecks txs a new CheckTx admitted.
		if checkTx(raw, abci.CheckTxType_New).Code == abci.CodeTypeOK {
			admitted = append(admitted, raw)
			// A recheck before the next block runs on the same check state.
			checkTx(raw, abci.CheckTxType_Recheck)
		}
	}

	commit()
	for i, raw := range admitted {
		resp := checkTx(raw, abci.CheckTxType_Recheck)
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
// bytes. A fast recheck app runs beside the stock app and must reach the same
// verdicts.
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

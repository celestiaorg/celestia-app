//go:build benchmarks

package benchmarks_test

import (
	"crypto/rand"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v11/app"
	"github.com/celestiaorg/celestia-app/v11/app/encoding"
	"github.com/celestiaorg/celestia-app/v11/app/frozen"
	"github.com/celestiaorg/celestia-app/v11/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v11/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v11/test/util"
	"github.com/celestiaorg/celestia-app/v11/test/util/testfactory"
	"github.com/cometbft/cometbft/abci/types"
	sdk "github.com/cosmos/cosmos-sdk/types"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	"github.com/stretchr/testify/require"
)

// frozenLists are the lists measured: none, the embedded list, and a ~4x margin.
var frozenLists = []struct {
	name  string
	addrs func() []sdk.AccAddress
}{
	{"none", func() []sdk.AccAddress { return nil }},
	{"embedded", embeddedAddrs},
	{"random160k", func() []sdk.AccAddress { return randomAddrs(160_000) }},
}

func embeddedAddrs() []sdk.AccAddress {
	bz, err := os.ReadFile("../frozen/addresses.txt")
	if err != nil {
		panic(err)
	}
	var out []sdk.AccAddress
	for _, line := range strings.Split(string(bz), "\n") {
		if line != "" && !strings.HasPrefix(line, "#") {
			out = append(out, sdk.MustAccAddressFromBech32(line))
		}
	}
	return out
}

func randomAddrs(n int) []sdk.AccAddress {
	out := make([]sdk.AccAddress, n)
	for i := range out {
		out[i] = make([]byte, 20)
		_, _ = rand.Read(out[i])
	}
	return out
}

// generateFixedSends signs count MsgSends to the whitehat at app version 11,
// from a sender added to list (or with an empty list if list is empty).
func generateFixedSends(b *testing.B, count int, list []sdk.AccAddress) (*app.App, [][]byte) {
	account := "test"
	cparams := app.DefaultConsensusParams()
	cparams.Version.App = frozen.ActivationVersion
	testApp, kr := testutil.SetupTestAppWithGenesisValSetAndMaxSquareSize(cparams, 128, account)
	addr := testfactory.GetAddress(kr, account)
	if len(list) == 0 {
		testApp.FrozenRules.SetList()
	} else {
		testApp.FrozenRules.SetList(append(list, addr)...)
	}
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	acc := testutil.DirectQueryAccount(testApp, addr)
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, user.NewAccount(account, acc.GetAccountNumber(), acc.GetSequence()))
	require.NoError(b, err)
	whitehat := sdk.MustAccAddressFromBech32(frozen.Whitehat)
	rawTxs := make([][]byte, 0, count)
	for range count {
		msg := banktypes.NewMsgSend(addr, whitehat, sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 10)))
		rawTx, _, err := signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(100_000), user.SetFee(1_000))
		require.NoError(b, err)
		rawTxs = append(rawTxs, rawTx)
		require.NoError(b, signer.IncrementSequence(account))
	}
	return testApp, rawTxs
}

func BenchmarkFinalizeBlock_FrozenSend_8MB(b *testing.B) {
	for _, l := range frozenLists {
		b.Run("list="+l.name, func(b *testing.B) {
			testApp, rawTxs := generateFixedSends(b, 31645, l.addrs())
			req := types.RequestFinalizeBlock{
				Time:   testutil.GenesisTime.Add(blockTime),
				Height: testApp.LastBlockHeight() + 1,
				Hash:   testApp.LastCommitID().Hash,
				Txs:    rawTxs,
			}
			b.ResetTimer()
			resp, err := testApp.FinalizeBlock(&req)
			require.NoError(b, err)
			b.StopTimer()
			for i := range rawTxs {
				require.Equal(b, uint32(0), resp.TxResults[i].Code, resp.TxResults[i].Log)
			}
		})
	}
}

func BenchmarkCheckTx_FrozenSend_8MB(b *testing.B) {
	for _, l := range frozenLists {
		b.Run("list="+l.name, func(b *testing.B) {
			testApp, rawTxs := generateFixedSends(b, 31645, l.addrs())
			_, err := testApp.FinalizeBlock(&types.RequestFinalizeBlock{
				Time:   testutil.GenesisTime.Add(blockTime),
				Height: testApp.LastBlockHeight() + 1,
				Hash:   testApp.LastCommitID().Hash,
			})
			require.NoError(b, err)
			_, err = testApp.Commit()
			require.NoError(b, err)
			b.ResetTimer()
			for _, tx := range rawTxs {
				resp, err := testApp.CheckTx(&types.RequestCheckTx{Tx: tx, Type: types.CheckTxType_New})
				require.NoError(b, err)
				require.Equal(b, uint32(0), resp.Code, resp.Log)
			}
		})
	}
}

func BenchmarkFrozenLookup(b *testing.B) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	miss := randomAddrs(1024)
	for _, l := range frozenLists[1:] {
		addrs := l.addrs()
		n := len(addrs)
		r := frozen.NewRules(enc.Codec)
		r.SetList(addrs...)
		b.Run(fmt.Sprintf("hit/n=%d", n), func(b *testing.B) {
			i := 0
			for b.Loop() {
				_ = r.Has(addrs[i%len(addrs)])
				i++
			}
		})
		b.Run(fmt.Sprintf("miss/n=%d", n), func(b *testing.B) {
			i := 0
			for b.Loop() {
				_ = r.Has(miss[i%len(miss)])
				i++
			}
		})
	}
}

func TestFrozenListFootprint(t *testing.T) {
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	runtime.GC()
	var e0, e1 runtime.MemStats
	runtime.ReadMemStats(&e0)
	start := time.Now()
	embedded := frozen.NewRules(enc.Codec) // first call parses the embedded list
	took := time.Since(start)
	runtime.GC()
	runtime.ReadMemStats(&e1)
	t.Logf("embedded list: parse=%s heap=+%.1f MiB", took, float64(e1.HeapAlloc-e0.HeapAlloc)/(1<<20))
	runtime.KeepAlive(embedded)
	for _, l := range frozenLists[1:] {
		addrs := l.addrs()
		n := len(addrs)
		runtime.GC()
		var m0, m1 runtime.MemStats
		runtime.ReadMemStats(&m0)
		start := time.Now()
		r := frozen.NewRules(enc.Codec)
		r.SetList(addrs...)
		took := time.Since(start)
		runtime.GC()
		runtime.ReadMemStats(&m1)
		t.Logf("n=%d build=%s heap=+%.1f MiB", n, took, float64(m1.HeapAlloc-m0.HeapAlloc)/(1<<20))
		runtime.KeepAlive(r)
	}
}

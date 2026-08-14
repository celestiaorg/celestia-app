//go:build benchmarks

package benchmarks_test

import (
	"bytes"
	"encoding/binary"
	"fmt"
	"testing"
	"time"

	"github.com/celestiaorg/celestia-app/v10/app"
	"github.com/celestiaorg/celestia-app/v10/app/encoding"
	"github.com/celestiaorg/celestia-app/v10/fibre"
	"github.com/celestiaorg/celestia-app/v10/pkg/appconsts"
	"github.com/celestiaorg/celestia-app/v10/pkg/user"
	testutil "github.com/celestiaorg/celestia-app/v10/test/util"
	"github.com/celestiaorg/celestia-app/v10/test/util/testfactory"
	fibretypes "github.com/celestiaorg/celestia-app/v10/x/fibre/types"
	"github.com/celestiaorg/go-square/v4/share"
	abci "github.com/cometbft/cometbft/abci/types"
	cmted25519 "github.com/cometbft/cometbft/crypto/ed25519"
	cmtproto "github.com/cometbft/cometbft/proto/tendermint/types"
	codectypes "github.com/cosmos/cosmos-sdk/codec/types"
	sdked25519 "github.com/cosmos/cosmos-sdk/crypto/keys/ed25519"
	"github.com/cosmos/cosmos-sdk/crypto/keys/secp256k1"
	sdk "github.com/cosmos/cosmos-sdk/types"
	"github.com/cosmos/cosmos-sdk/types/tx/signing"
	banktypes "github.com/cosmos/cosmos-sdk/x/bank/types"
	stakingtypes "github.com/cosmos/cosmos-sdk/x/staking/types"
	"github.com/stretchr/testify/require"

	sdkmath "cosmossdk.io/math"
)

// These benchmarks answer one question: do the flat PFF gas charges in
// pkg/appconsts/fibre_gas_consts.go match the real work?
//
// Method: time the PFF work, then compare against what a gas buys in a
// normal tx (MsgSend). See results_fibre.md for the numbers.
//
// Run with:
//
//	go test -tags=benchmarks -bench=PFF -run=^$ ./app/benchmarks/...

const (
	// pffBlobSize affects the payment amount, not the measured compute.
	pffBlobSize = 100

	// pffEscrowSeed covers every payment in a benchmark run.
	pffEscrowSeed = 1_000_000_000_000
)

// pffBenchEnv holds the test app and funded signer.
type pffBenchEnv struct {
	testApp *app.App
	signer  *user.Signer
	account string
	addr    sdk.AccAddress
}

func setupPFFBenchEnv(b testing.TB) *pffBenchEnv {
	account := "test"
	testApp, kr := testutil.SetupTestAppWithGenesisValSet(app.DefaultConsensusParams(), account)
	addr := testfactory.GetAddress(kr, account)
	enc := encoding.MakeConfig(app.ModuleEncodingRegisters...)
	acc := testutil.DirectQueryAccount(testApp, addr)
	signer, err := user.NewSigner(kr, enc.TxConfig, testutil.ChainID, user.NewAccount(account, acc.GetAccountNumber(), acc.GetSequence()))
	require.NoError(b, err)

	env := &pffBenchEnv{testApp: testApp, signer: signer, account: account, addr: addr}
	env.seedEscrow(b, pffEscrowSeed)
	return env
}

// uncachedCtx returns a context over committed state for direct keeper writes.
func (e *pffBenchEnv) uncachedCtx() sdk.Context {
	return e.testApp.NewUncachedContext(false, cmtproto.Header{
		ChainID: testutil.ChainID,
		Height:  e.testApp.LastBlockHeight(),
		Time:    time.Now(),
	})
}

func (e *pffBenchEnv) seedEscrow(b testing.TB, amount int64) {
	ctx := e.uncachedCtx()
	coins := sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, amount))
	require.NoError(b, e.testApp.BankKeeper.SendCoinsFromAccountToModule(ctx, e.addr, fibretypes.ModuleName, coins))
	e.testApp.FibreKeeper.SetEscrowAccount(ctx, fibretypes.EscrowAccount{
		Signer:           e.addr.String(),
		Balance:          coins[0],
		AvailableBalance: coins[0],
	})
}

// newPFFMsg creates a valid PFF message. uniq makes its promise hash unique.
func (e *pffBenchEnv) newPFFMsg(b testing.TB, height int64, uniq uint64) *fibretypes.MsgPayForFibre {
	acc := e.signer.Account(e.account)
	pubKey, ok := acc.PubKey().(*secp256k1.PubKey)
	require.True(b, ok)

	// Give each promise a unique hash.
	commitment := bytes.Repeat([]byte{0xAB}, share.FibreCommitmentSize)
	binary.BigEndian.PutUint64(commitment[:8], uniq)
	ns := share.MustNewV0Namespace(bytes.Repeat([]byte{0x01}, share.NamespaceVersionZeroIDSize))

	msg := &fibretypes.MsgPayForFibre{
		Signer: e.addr.String(),
		PaymentPromise: fibretypes.PaymentPromise{
			ChainId:           testutil.ChainID,
			Height:            height,
			Namespace:         ns.Bytes(),
			BlobSize:          pffBlobSize,
			BlobVersion:       fibretypes.BlobVersionZero,
			Commitment:        commitment,
			CreationTimestamp: time.Now().Truncate(time.Second),
			SignerPublicKey:   *pubKey,
			Signature:         make([]byte, 64),
		},
	}

	signBytes := promiseSignBytes(b, msg)
	var err error
	msg.PaymentPromise.Signature, _, err = e.signer.Keyring().Sign(e.account, signBytes, signing.SignMode_SIGN_MODE_DIRECT)
	require.NoError(b, err)

	validatorSignature, err := testutil.GenesisValidatorPrivateKey().Sign(signBytes)
	require.NoError(b, err)
	msg.ValidatorSignatures = [][]byte{validatorSignature}
	return msg
}

// promiseSignBytes returns the payment promise's canonical sign bytes.
func promiseSignBytes(b testing.TB, msg *fibretypes.MsgPayForFibre) []byte {
	pp := fibre.PaymentPromise{}
	require.NoError(b, pp.FromProto(&msg.PaymentPromise))
	signBytes, err := pp.SignBytes()
	require.NoError(b, err)
	return signBytes
}

// setupHistoricalValset stores n validators at height and returns their keys
// in validator-set order.
func (e *pffBenchEnv) setupHistoricalValset(b testing.TB, height int64, n int) []cmted25519.PrivKey {
	ctx := e.uncachedCtx()
	privKeys := make([]cmted25519.PrivKey, n)
	valset := make([]stakingtypes.Validator, n)
	for i := range n {
		privKeys[i] = cmted25519.GenPrivKey()
		consPubKey := &sdked25519.PubKey{Key: privKeys[i].PubKey().Bytes()}
		pkAny, err := codectypes.NewAnyWithValue(consPubKey)
		require.NoError(b, err)
		valset[i] = stakingtypes.Validator{
			OperatorAddress: sdk.ValAddress(consPubKey.Address()).String(),
			ConsensusPubkey: pkAny,
			Status:          stakingtypes.Bonded,
			Tokens:          sdkmath.NewInt(1),
		}
	}
	historicalInfo := stakingtypes.HistoricalInfo{
		Header: cmtproto.Header{ChainID: testutil.ChainID, Height: height},
		Valset: valset,
	}
	require.NoError(b, e.testApp.StakingKeeper.SetHistoricalInfo(ctx, height, &historicalInfo))
	return privKeys
}

// newPFFMsgWithValset creates a PFF message signed by every provided validator.
func (e *pffBenchEnv) newPFFMsgWithValset(b testing.TB, height int64, uniq uint64, privKeys []cmted25519.PrivKey) *fibretypes.MsgPayForFibre {
	msg := e.newPFFMsg(b, height, uniq)
	signBytes := promiseSignBytes(b, msg)
	signatures := make([][]byte, len(privKeys))
	for i, privKey := range privKeys {
		sig, err := privKey.Sign(signBytes)
		require.NoError(b, err)
		signatures[i] = sig
	}
	msg.ValidatorSignatures = signatures
	return msg
}

// signTx signs msg as a transaction and advances the nonce.
func (e *pffBenchEnv) signTx(b testing.TB, msg sdk.Msg, gasLimit uint64) []byte {
	txBytes, _, err := e.signer.CreateTx([]sdk.Msg{msg}, user.SetGasLimit(gasLimit), user.SetFee(10_000))
	require.NoError(b, err)
	require.NoError(b, e.signer.IncrementSequence(e.account))
	return txBytes
}

// finalizeBlock times FinalizeBlock over txs (tx building, assertions, and
// Commit stay off the clock), requires every tx to succeed, and returns the
// last tx's gas. Callers stop the timer before building txs.
func (e *pffBenchEnv) finalizeBlock(b *testing.B, txs [][]byte) int64 {
	req := &abci.RequestFinalizeBlock{
		Time:   time.Now(),
		Height: e.testApp.LastBlockHeight() + 1,
		Hash:   e.testApp.LastCommitID().Hash,
		Txs:    txs,
	}
	b.StartTimer()
	resp, err := e.testApp.FinalizeBlock(req)
	b.StopTimer()
	require.NoError(b, err)
	require.Len(b, resp.TxResults, len(txs))
	var gasUsed int64
	for i, res := range resp.TxResults {
		require.Equal(b, abci.CodeTypeOK, res.Code, "tx %d: %s", i, res.Log)
		gasUsed = res.GasUsed
	}
	_, err = e.testApp.Commit()
	require.NoError(b, err)
	return gasUsed
}

// BenchmarkFinalizeBlock_PFF compares cached and uncached PFF signatures in
// FinalizeBlock. Both paths must charge the same gas.
func BenchmarkFinalizeBlock_PFF(b *testing.B) {
	b.Run("uncached signatures", func(b *testing.B) { benchmarkFinalizeBlockPFF(b, false) })
	b.Run("cached signatures", func(b *testing.B) { benchmarkFinalizeBlockPFF(b, true) })
}

// The block benchmarks use explicit b.N loops with manual timer control so
// that only FinalizeBlock is on the clock; the CPU-only benchmarks further
// down use b.Loop, which times the whole loop body.
func benchmarkFinalizeBlockPFF(b *testing.B, cached bool) {
	env := setupPFFBenchEnv(b)

	var gasUsed int64
	var txSize int
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		// The promise height must stay within PaymentPromiseHeightWindow of the
		// executing height, so pin it to the last committed height.
		tx := env.signTx(b, env.newPFFMsg(b, env.testApp.LastBlockHeight(), uint64(i)), 1_000_000)
		txSize = len(tx)
		if cached {
			resp, err := env.testApp.CheckTx(&abci.RequestCheckTx{Tx: tx, Type: abci.CheckTxType_New})
			require.NoError(b, err)
			require.Equal(b, abci.CodeTypeOK, resp.Code, resp.Log)
		}
		gasUsed = env.finalizeBlock(b, [][]byte{tx})
	}
	b.ReportMetric(float64(gasUsed), "gas_used")
	b.ReportMetric(float64(txSize), "transaction_size(byte)")
}

// BenchmarkFinalizeBlock_PFFBaselines measures empty and MsgSend blocks for
// comparison with PFF blocks.
func BenchmarkFinalizeBlock_PFFBaselines(b *testing.B) {
	b.Run("empty block", benchmarkFinalizeBlockEmpty)
	b.Run("single MsgSend", benchmarkFinalizeBlockMsgSend)
}

// benchmarkFinalizeBlockEmpty measures the per-block overhead with no txs.
func benchmarkFinalizeBlockEmpty(b *testing.B) {
	env := setupPFFBenchEnv(b)

	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		env.finalizeBlock(b, nil)
	}
}

// benchmarkFinalizeBlockMsgSend measures a normal tx as the price reference:
// it tells us how much work one gas buys elsewhere on the chain, so we can
// judge the PFF charges against it. (We can't use a PFF as its own
// reference — its price contains the very constants being checked.)
func benchmarkFinalizeBlockMsgSend(b *testing.B) {
	env := setupPFFBenchEnv(b)

	var gasUsed int64
	b.ResetTimer()
	for range b.N {
		b.StopTimer()
		sendMsg := banktypes.NewMsgSend(env.addr, env.addr, sdk.NewCoins(sdk.NewInt64Coin(appconsts.BondDenom, 1)))
		gasUsed = env.finalizeBlock(b, [][]byte{env.signTx(b, sendMsg, 200_000)})
	}
	b.ReportMetric(float64(gasUsed), "gas_used")
}

// BenchmarkFinalizeBlock_PFFWorstCase measures 200 uncached PFF transactions
// signed by 100 validators.
func BenchmarkFinalizeBlock_PFFWorstCase(b *testing.B) {
	const (
		// Production limit; the benchmark build tag removes it.
		pffsPerBlock  = 200
		numValidators = 100
	)
	env := setupPFFBenchEnv(b)

	// Ante verifies against this historical validator set.
	height := env.testApp.LastBlockHeight()
	privKeys := env.setupHistoricalValset(b, height, numValidators)

	var uniq uint64
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		b.StopTimer()
		txs := make([][]byte, pffsPerBlock)
		for j := range txs {
			txs[j] = env.signTx(b, env.newPFFMsgWithValset(b, height, uniq, privKeys), 1_000_000)
			uniq++
		}
		env.finalizeBlock(b, txs)
	}
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N)/1e6, "ms/block")
	b.ReportMetric(float64(b.Elapsed().Nanoseconds())/float64(b.N*pffsPerBlock)/1e3, "µs/pff")
}

// BenchmarkPFFSignatureVerification compares verification time and charged
// gas across validator-set sizes.
func BenchmarkPFFSignatureVerification(b *testing.B) {
	env := setupPFFBenchEnv(b)

	for _, n := range []int{1, 10, 30, 100} {
		b.Run(fmt.Sprintf("%d_validators", n), func(b *testing.B) {
			// Keep historical validator sets separate.
			height := int64(10_000 + n)
			privKeys := env.setupHistoricalValset(b, height, n)
			msg := env.newPFFMsgWithValset(b, height, 0, privKeys)
			ctx := env.uncachedCtx()

			b.ResetTimer()
			for b.Loop() {
				if err := env.testApp.FibreKeeper.ValidatePayForFibreSignatures(ctx, msg); err != nil {
					b.Fatal(err)
				}
			}
			b.StopTimer()
			b.ReportMetric(float64(appconsts.PFFibreTxGasFixedCost+uint64(n)*appconsts.PFFibreGasPerValidatorSignature), "gas_charged")
		})
	}
}

// TestPFFGasConstantsVsMeasuredCost compares what we price as gas
// (PFFibreTxGasFixedCost, PFFibreGasPerValidatorSignature) with what the work
// actually costs, and prints both side by side. It never fails; it only
// reports. If the implied values drift far from the constants, retune the
// constants.
//
// Run with:
//
//	go test -tags=benchmarks -run=TestPFFGasConstantsVsMeasuredCost -v ./app/benchmarks/
func TestPFFGasConstantsVsMeasuredCost(t *testing.T) {
	env := setupPFFBenchEnv(t)
	ctx := env.uncachedCtx()

	// Step 1: the exchange rate.
	anchorGas := env.testApp.AccountKeeper.GetParams(ctx).SigVerifyCostSecp256k1
	anchorMsg := env.newPFFMsg(t, env.testApp.LastBlockHeight(), 0)
	signBytes := promiseSignBytes(t, anchorMsg)
	pubKey, sig := anchorMsg.PaymentPromise.SignerPublicKey, anchorMsg.PaymentPromise.Signature
	anchorNs := timeIt(func() bool { return pubKey.VerifySignature(signBytes, sig) })
	nsPerGas := anchorNs / float64(anchorGas)

	// Step 2: time PFF verification at several signature counts.
	counts := []int{1, 25, 50, 100}
	workNs := make([]float64, len(counts))
	for i, n := range counts {
		height := int64(20_000 + n) // a separate historical valset per count
		privKeys := env.setupHistoricalValset(t, height, n)
		msg := env.newPFFMsgWithValset(t, height, uint64(n), privKeys)
		workNs[i] = timeIt(func() bool {
			return env.testApp.FibreKeeper.ValidatePayForFibreSignatures(ctx, msg) == nil
		})
		t.Logf("verification with %3d signatures: %.0f ns", n, workNs[i])
	}

	// Step 3: split into fixed + per-signature and convert to gas.
	fixedNs, perSigNs := leastSquaresFit(counts, workNs)
	t.Logf("exchange rate: %.2f ns per gas (one secp256k1 verify: %.0f ns = %d gas)", nsPerGas, anchorNs, anchorGas)
	t.Logf("fit: work_ns(n) = %.0f + %.0f*n", fixedNs, perSigNs)
	t.Logf("implied fixed gas: %.0f (current PFFibreTxGasFixedCost: %d)",
		fixedNs/nsPerGas, appconsts.PFFibreTxGasFixedCost)
	t.Logf("implied per-signature gas: %.0f (current PFFibreGasPerValidatorSignature: %d)",
		perSigNs/nsPerGas, appconsts.PFFibreGasPerValidatorSignature)
}

// timeIt returns the nanoseconds per call of fn, which must return true on
// success.
func timeIt(fn func() bool) float64 {
	res := testing.Benchmark(func(b *testing.B) {
		for range b.N {
			if !fn() {
				b.Fatal("measured operation failed")
			}
		}
	})
	return float64(res.NsPerOp())
}

// leastSquaresFit returns the intercept and slope for x and y.
func leastSquaresFit(x []int, y []float64) (intercept, slope float64) {
	n := float64(len(x))

	var meanX, meanY float64
	for i := range x {
		meanX += float64(x[i])
		meanY += y[i]
	}
	meanX /= n
	meanY /= n

	var covXY, varX float64
	for i := range x {
		dx := float64(x[i]) - meanX
		covXY += dx * (y[i] - meanY)
		varX += dx * dx
	}

	slope = covXY / varX
	intercept = meanY - slope*meanX
	return intercept, slope
}
